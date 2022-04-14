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
	"path/filepath"
	"regexp"
	"sort"
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

	index_file, err := os.Create(path.Join(datadir, idx_file))
	if err != nil {
		log.Fatal(err)
	}
	defer index_file.Close()

	var file_names []string
	binlog_cnt := 0
	binlog_re, _ := regexp.Compile(`\.[0-9]{6}`)
	for _, v := range files {
		if !v.IsDir() {
			if matched := binlog_re.MatchString(string(v.Name())); !matched {
				continue
			}

			binlog_cnt++
			dstName := relay_log_name + filepath.Ext(v.Name())
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

var backup_tar = flag.String("backup-tar", "", "Remove data directory and unpack tar.gz or tar.* archive with MySQL data files")
var binlog_dir = flag.String("binlog-directory", "", "Binary logs directory location")

func main() {
	flag.Parse()

	println(*backup_tar)

	datadir, relay_log_index := get_mysql_vars()

	if *binlog_dir == "" {
		log.Fatal("PiTR binary logs directory is not defined, use --binlog-directory <location>")
	}

	if *backup_tar != "" {
		restore_full_backup_tar(*backup_tar, datadir)
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
	run_fatal("can't setup replication", "", "mysql", "-e", "CHANGE MASTER TO RELAY_LOG_FILE='"+first_binlog+"', RELAY_LOG_POS=1, MASTER_HOST='dummy' FOR CHANNEL '';START SLAVE SQL_THREAD FOR CHANNEL ''")
}
