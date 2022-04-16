# mysql_pitr_restore

MySQL point in time recovery is based on replication logs (binary logs). Full backup with GTID configured could be restored from mysqldump/mydumper/mysqlshell dump/restore or full filesystem copy while the database is not running. Up to date changes are applied from binary logs. The program uses [Howto make MySQL point-in-time recovery faster ?](https://lefred.be/content/howto-make-mysql-point-in-time-recovery-faster/) method to apply binary logs.
You can specify the directory with binary logs and optional `tar.gz` or `tar.*` file with data directory archive.

## Build

```
go build .
```

## Using existing data directory

```
[root@ihanick-default mysql_pitr_restore]# systemctl stop mysql
[root@ihanick-default mysql_pitr_restore]# rm -rf /var/lib/mysql/ ; tar -C / -xaf /root/full-backup.tar.gz ; systemctl start mysql
[root@ihanick-default mysql_pitr_restore]# ./mysql_pitr_restore --binlog-directory=/root/mysql

2022/04/14 20:19:53 mysqld --verbose --help
2022/04/14 20:19:53 Using existing database
2022/04/14 20:19:53 Relay log index file ihanick-default-relay-bin.index
2022/04/14 20:19:58 chown mysql -R /var/lib/mysql/
2022/04/14 20:19:58 mysql -e 'CHANGE MASTER TO RELAY_LOG_FILE='"'"'mysql1-relay-bin.000001'"'"', RELAY_LOG_POS=1, MASTER_HOST='"'"'dummy'"'"' FOR CHANNEL '"'"''"'"';START SLAVE SQL_THREAD FOR CHANNEL '"'"''"'"''
```

## Using tar.gz archive (or any tar.XX)

```
[root@ihanick-default mysql_pitr_restore]# go build . && ./mysql_pitr_restore --backup-tar=/root/full-backup.tar.gz --binlog-directory=/root/mysql
/root/full-backup.tar.gz
2022/04/14 20:18:59 mysqld --verbose --help
2022/04/14 20:18:59 systemctl stop mysql
2022/04/14 20:19:01 rm -rf /var/lib/mysql/
2022/04/14 20:19:01 tar -C / -xaf /root/full-backup.tar.gz
2022/04/14 20:19:02 systemctl start mysql
2022/04/14 20:19:04 Relay log index file ihanick-default-relay-bin.index
2022/04/14 20:19:10 chown mysql -R /var/lib/mysql/
2022/04/14 20:19:10 mysql -e 'CHANGE MASTER TO RELAY_LOG_FILE='"'"'mysql1-relay-bin.000001'"'"', RELAY_LOG_POS=1, MASTER_HOST='"'"'dummy'"'"' FOR CHANNEL '"'"''"'"';START SLAVE SQL_THREAD FOR CHANNEL '"'"''"'"''
```

## Download backup from S3 storage

Backups should be uploaded to S3 with [xbcloud](https://www.percona.com/doc/percona-xtrabackup/LATEST/xbcloud/xbcloud.html).
Use xbcloud and xbstream to download backup. Please install appropriate xtrabackup version.

```
./mysql_pitr_restore --binlog-directory=/root/binlogs \
  --storage=s3 \
  --s3-region=us-east-1 \
  --s3-endpoint=minio-service.default.svc.cluster.local:9000 \
  --s3-access-key=REPLACE-WITH-AWS-ACCESS-KEY \
  --s3-secret-key=REPLACE-WITH-AWS-SECRET-KEY \
  --s3-bucket=operator-testing \
  --s3-bucket-lookup=path \
  --s3-backup-directory=cluster1-2022-04-15-00:18:49-full
```

### Getting binary logs created by Percona Kubernetes Operator for PXC

```
./mysql_pitr_restore \
  --binlog-directory=/root/binlogs1 \
  --storage=s3 --s3-region=us-east-1 \
  --s3-endpoint=minio-service.default.svc.cluster.local:9000 \
  --s3-access-key=REPLACE-WITH-AWS-ACCESS-KEY \
  --s3-secret-key=REPLACE-WITH-AWS-SECRET-KEY \
  --s3-bucket=operator-testing \
  --s3-bucket-lookup=path \
  --s3-backup-directory=cluster1-2022-04-15-00:18:49-full \
  --binlog-s3-endpoint=minio-service.default.svc.cluster.local:9000 \
  --binlog-s3-bucket=operator-testing \
  --binlog-s3-access-key=REPLACE-WITH-AWS-ACCESS-KEY \
  --binlog-s3-secret-key=REPLACE-WITH-AWS-SECRET-KEY
```
