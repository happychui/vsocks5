cd /D ./main
set GOPROXY=https://goproxy.cn,https://mirrors.aliyun.com/goproxy/,https://gocenter.io,https://proxy.golang.org,https://goproxy.io,https://athens.azurefd.net,direct
set GOSUMDB=sum.golang.org

go mod tidy

set CGO_ENABLED=0

set GOOS=windows
set GOARCH=amd64
go build -o ../bin/vsocks5-win-amd64.exe -ldflags="-s -w"
go clean -cache

rem set GOOS=linux
rem set GOARCH=amd64
rem go build -o ../bin/vsocks5-linux-amd64 -ldflags="-s -w"
rem set GOARCH=386
rem go build -o ../bin/vsocks5-linux-386 -ldflags="-s -w"
rem set GOARCH=arm
rem set GOARM=7
rem go build -o ../bin/vsocks5-linux-armv7 -ldflags="-s -w"
rem set GOARCH=arm64
rem go build -o ../bin/vsocks5-linux-arm64 -ldflags="-s -w"
rem set GOARCH=mips
rem go build -o ../bin/vsocks5-linux-mips -ldflags="-s -w"
rem go clean -cache

rem upx -9 ../bin/*