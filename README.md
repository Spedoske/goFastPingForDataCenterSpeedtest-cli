# goFastPingForDataCenterSpeedtest-cli
编译参数：
```
go mod init goPing
go build -buildmode=c-shared -o ping.so main.go
```

输出文件包括`ping.h`和`ping.so`
