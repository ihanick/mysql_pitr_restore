package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alessio/shellescape"
)

var TIMEOUT time.Duration = 600

func copy_file(src string, dest string) {
	r, err := os.Open(src)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()
	w, err := os.Create(dest)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()
	w.ReadFrom(r)
	if err := w.Sync(); err != nil {
		log.Fatal("Can't fsync binary log file", err)
	}
}

func copy_binlogs(src_dir string, datadir string, idx_file string, first_binlog *string) {
	relay_log_name := "mysql1-relay-bin"
	f, err := os.Open(src_dir)
	if err != nil {
		log.Fatal(err)
	}
	files, err := f.Readdir(0)
	if err != nil {
		log.Fatal(err)
	}

	index_file, err := os.OpenFile(path.Join(datadir, idx_file), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		log.Fatal(err)
	}
	defer index_file.Close()

	var file_names []string
	binlog_cnt := 0
	binlog_re, _ := regexp.Compile(`\.[0-9]{6}|binlog_1`)
	for _, v := range files {
		if !v.IsDir() {
			if strings.HasSuffix(string(v.Name()), "-gtid-set") {
				continue
			}
			if matched := binlog_re.MatchString(string(v.Name())); !matched {
				continue
			}

			binlog_cnt++
			dstName := relay_log_name + fmt.Sprintf(".%06d", binlog_cnt)
			copy_file(path.Join(src_dir, v.Name()), path.Join(datadir, dstName))
			file_names = append(file_names, dstName)
		}
	}

	if binlog_cnt == 0 {
		log.Fatal("No binary logs found in ", src_dir)
	}

	sort.Strings(file_names)
	*first_binlog = file_names[0]
	for _, file_name := range file_names {
		if _, err = io.WriteString(index_file, file_name+"\n"); err != nil {
			log.Fatal(err)
		}
	}
	if err := index_file.Sync(); err != nil {
		log.Fatal(err)
	}
}

func run_fatal(error_msg string, ignore_msg string, cmd_line string, args ...string) {
	command_txt := shellescape.Quote(cmd_line)
	for _, s := range args {
		command_txt += " " + shellescape.Quote(s)
	}
	log.Print(command_txt)

	ctx, cancel := context.WithTimeout(context.Background(), TIMEOUT*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, cmd_line, args...).CombinedOutput()

	if err != nil {
		matched, _ := regexp.MatchString(ignore_msg, string(out))
		if ignore_msg != "" && matched {
			fmt.Println("ignoring error")
		} else {
			log.Println(string(out))
			log.Fatal(error_msg, ": ", err)
		}
	}
}

func run_get_line(error_msg string, ignore_msg string, cmd_line string, args ...string) (string, error) {
	command_txt := shellescape.Quote(cmd_line)
	for _, s := range args {
		command_txt += " " + shellescape.Quote(s)
	}
	log.Print(command_txt)

	ctx, cancel := context.WithTimeout(context.Background(), TIMEOUT*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, cmd_line, args...).CombinedOutput()

	if err != nil {
		matched, _ := regexp.MatchString(ignore_msg, string(out))
		if ignore_msg != "" && matched {
			fmt.Println("ignoring error")
		} else {
			log.Println(string(out))
			log.Fatal(error_msg, ": ", err)
		}
	}

	return string(out[:]), nil
}

func get_mysql_vars() (string, string) {
	out, err := run_get_line("relay log index location", "", "mysqld", "--verbose", "--help")
	if err != nil {
		log.Fatal("Can't determine relay log index file name", err, out)
	}

	r_relay_idx := regexp.MustCompile(`relay-log-index \s*(.*)`)
	matches := r_relay_idx.FindAllStringSubmatch(out, -1)
	relay_idx := ""
	for _, v := range matches {
		relay_idx = v[1]
	}

	r_datadir := regexp.MustCompile(`datadir \s*(.*)`)
	datadir_matches := r_datadir.FindAllStringSubmatch(out, -1)
	datadir := ""
	for _, v := range datadir_matches {
		datadir = v[1]
	}

	return datadir, relay_idx
}

func restore_full_backup_tar(backup_tar string, datadir string) {
	if _, err := os.Stat(backup_tar); err != nil {
		log.Fatal("Can't find backup archive: ", err)
	}

	run_fatal("can't stop mysqld", "", "systemctl", "stop", "mysql")
	run_fatal("cleanup data directory", "", "rm", "-rf", datadir)
	run_fatal("restore full backup", "", "tar", "-C", "/", "-xaf", backup_tar)
	run_fatal("can't start mysqld", "", "systemctl", "start", "mysql")
}

type BackupS3 struct {
	Region          string
	Endpoint        string
	AccessKey       string
	SecretKey       string
	Bucket          string
	BucketLookup    string
	BackupDirectory string
}

func restore_full_backup_s3(conn BackupS3, datadir string) {
	run_fatal("can't stop mysqld", "", "systemctl", "stop", "mysql")
	run_fatal("cleanup data directory", "", "rm", "-rf", datadir)
	run_fatal("create data directory", "", "mkdir", datadir)
	restore_cmd := fmt.Sprintf("xbcloud get --storage=s3 --s3-region=%s --s3-endpoint=%s --s3-access-key=%s --s3-secret-key=%s --s3-bucket=%s --s3-bucket-lookup=%s %s | xbstream -x -C %s --parallel=8",
		shellescape.Quote(conn.Region),
		shellescape.Quote(conn.Endpoint),
		shellescape.Quote(conn.AccessKey),
		shellescape.Quote(conn.SecretKey),
		shellescape.Quote(conn.Bucket),
		shellescape.Quote(conn.BucketLookup),
		shellescape.Quote(conn.BackupDirectory),
		shellescape.Quote(datadir))
	run_fatal("", "", "sh", "-c", restore_cmd)
	run_fatal("Can't prepare backup", "", "xtrabackup", "--prepare", "--target-dir", datadir)
	run_fatal("can't change ownship", "", "chown", "mysql", "-R", datadir)
	run_fatal("can't start mysqld", "", "systemctl", "start", "mysql")
}

var backup_tar = flag.String("backup-tar", "", "Remove data directory and unpack tar.gz or tar.* archive with MySQL data files")
var binlog_dir = flag.String("binlog-directory", "", "Binary logs directory location")
var storage_type = flag.String("storage", "", "Storage type, e.g. s3")
var s3_region = flag.String("s3-region", "us-east-1", "s3 region")
var s3_endpoint = flag.String("s3-endpoint", "", "s3 endpoint for https connection in domain:port format")
var s3_access_key = flag.String("s3-access-key", "", "s3 access key")
var s3_secret_key = flag.String("s3-secret-key", "", "s3 secret key")
var s3_bucket = flag.String("s3-bucket", "", "s3 bucket")
var s3_bucket_lookup = flag.String("s3-bucket-lookup", "path", "s3 bucket lookup e.g. path")
var s3_backup_dir = flag.String("s3-backup-directory", "", "backup path in S3 bucket")

func main() {
	flag.Parse()

	datadir, relay_log_index := get_mysql_vars()

	if *binlog_dir == "" {
		log.Fatal("PiTR binary logs directory is not defined, use --binlog-directory <location>")
	}

	if *backup_tar != "" {
		restore_full_backup_tar(*backup_tar, datadir)
	} else if *storage_type == "s3" {
		restore_full_backup_s3(BackupS3{*s3_region, *s3_endpoint, *s3_access_key, *s3_secret_key, *s3_bucket, *s3_bucket_lookup, *s3_backup_dir}, datadir)
	} else {
		log.Print("Using existing database")
	}

	log.Printf("Relay log index file %s", relay_log_index)
	if relay_log_index == "" {
		log.Fatal("Can't find relay log files index location")
	}
	first_binlog := ""
	copy_binlogs(*binlog_dir, datadir, relay_log_index, &first_binlog)
	run_fatal("can't change ownship", "", "chown", "mysql", "-R", datadir)
	run_fatal("can't setup replication", "", "mysql", "-e", "SET GLOBAL server_id=UNIX_TIMESTAMP();CHANGE MASTER TO RELAY_LOG_FILE='"+first_binlog+"', RELAY_LOG_POS=1, MASTER_HOST='dummy' FOR CHANNEL '';START SLAVE SQL_THREAD FOR CHANNEL ''")
}
