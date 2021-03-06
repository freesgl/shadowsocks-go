shadowsocks-go
--------------
自用的 shadowsocks,使用 golang 开发

Features
--------

* 实现了完整的 shadowsocks 协议(没有支持 AEAD 的计划,因为原始的 ss 协议已经足够安全可靠)  
* 解决 ss 服务器可能被探测攻击的问题  
* 与官方的 ss 实现相互兼容,注意在使用客户端连接官方的 ss 服务器时必须设置 nonop 选项为 true  
* 支持 aes-128/192/256-ctr aes-128/192/256-cfb chacha20 chacha20-ietf rc4-md5 salsa20 加密  
* 支持代理中继功能,可以从多个 ss 服务端中选取一个响应最快的服务端建立连接  
* 支持 TCP 隧道(如 ssh over shadowsocks)  
* 支持 UDP Tunnel
* 支持 TCP redirect,类似 ss-libev 的 redir 模式  
* 支持单个端口设置不同的加密方式及密码  
* 配置文件更改后自动重载配置文件,并且不影响已经建立的连接  
* 实现了简单的 HTTP 伪装,伪装的目的是欺骗运营商而不是绕过某些设备,应当谨慎使用    
* 本地客户端支持 HTTP/socks4/socks4a 代理请求(不需要配置，默认启用，与 socks5 协议共存)  
* 支持 HTTP 请求记录，能够记录经过代理程序的 HTTP 请求的请求头(不包含 body 部分)  
* 在代理 HTTPS 流量时，可以仅加密前16384个字节的数据，提升转发性能  
* 支持设置 ChnRoute 文件，IP 地址命中 ChnRoute 文件中指定的 IP 段时直接连接  
* 可以设置黑白名单域名文件，命中黑名单的域名走代理，命中白名单的直接连接，同时程序会每分钟更新一次黑白名单中的内容
* 支持 HTTP/HTTPS MITM 反代，通过解析 HTTP/HTTPS 请求，获取真实的目的域名
* 兼容 shadowsocks/simple-obfs(tls/http)  
* 实现了前向纠错码，用于降低 UDP 流量的丢包率  

Build
-----
```
go get -u -v github.com/ccsexyz/shadowsocks-go  
```

Usage
-----
```
shadowsocks-go configfile
```

shadowsocks-go 使用 json 配置文件,配置文件的基本单位为一个 json object,一个配置文件中可以只有一个 object,也可以是由多个 json object 组成的数组
```json
// ok
{
  "localaddr": ":1080",
  "remoteaddr": "vps:8888",
  "method": "chacha20",
  "password": "you need a password",
  "nonop": true
}
```
```json
//also ok
[
  {
    "localaddr": ":1080",
    "remoteaddr": "vps:9999"
  },
  {
    "type": "tcptun",
    "localaddr": ":2222",
    "remoteaddr": "ssh-server:ssh-port",
    "backend": {
      "remoteaddr": "vps:8888",
      "password": "you need a password",
      "method": "chacha20",
      "nonop": true
    }
  }
]
```

json 对象中的可选配置:
* type: 服务端或客户端的类型,如果是 "server" 或者 "local" 可以省略不填  
* localaddr: 本地监听的地址  
* remoteaddr: ss 服务端地址,在 tcptun 中为隧道的目的地址, 在 *proxy 中这个参数不起作用  
* method: 加密方式,默认为 aes-256-cfb     
* password: 密码  
* nonop: 我实现的 ss 客户端在发送第一段数据时会向服务端发送一段随机数据,设置这个选项为 true 以和官方的 ss 服务端兼容   
* udprelay: 设置为 true 时启用 udp 转发功能,默认行为为不启用  
* backend: 用在 tcptun 中,设置用于转发的 ss 服务端的配置信息  
* backends: 用于 *proxy 中,设置一组用于转发的 ss 服务端的配置信息  
* obfs: 设置是否启用 HTTP 伪装,服务端开启此选项后仍然兼容不适用 HTTP 伪装的客户端    
* obfshost: 设置一组 HOST,用于伪装中的 HOST 字段  
* obfsalive: 设置为 true 时将启用 HTTP-keepalive,复用已经建立的空闲连接  
* logfile: 设置日志文件的输出路径  
* verbose: 输出详细日志  
* debug: 输出调试日志  
* mux: 设置是否启用多路复用功能,设置为 true 时可由一个 TCP 连接承载多个 ss 代理连接  
* loghttp: 设置为 true 时将记录经过代理的 HTTP 请求的请求头部分  
* partenchttps: 开启后在传输 HTTPS 流量时仅会加密前 16384 个字节的数据  
* snappy: 启用 snappy 数据流压缩   
* chnlist: 设置 ChnRoute 文件地址，命中的 IP 将直接连接，不通过代理进行请求    
* autoproxy: 启用后会根据连接延迟决定是否直接连接  
* proxylist: 黑名单文件路径,存储需要走代理的域名  
* notproxylist: 白名单文件路径,存储需要直接连接的域名  
* partenc: 开启后仅仅加密 HTTPS 流量前 16384 个字节的数据
* mitm: 开启 MITM 反向代理功能

type 字段的可选值:  
* local: ss 客户端
* server: ss 服务端  
* ssproxy: ss 代理,前端是一个 ss 服务器  
* socksproxy: ss 代理,前端是一个 socks5/socks4/socks4a/http 服务器  
* tcptun: TCP 隧道服务器   
* udptun: UDP 隧道服务器    
* redir: TCP redirect 本地客户端,使用方法参考 ss-libev 项目中的 ss-redir  
* multiserver: 单个端口多加密方式及密码的 ss 服务端, 注意 aes 加密的 ctr 和 cfb 模式不能设置为同一个密码,因为这两种模式的第一个分组的解密结果是相同的,无法区分  

具体的使用可以参考 sample-config 中的示例配置文件  

Advanced Usage
--------------

1.通过 Nginx 转发流量  
在设置 obfs 的情况下可以在服务端的前方启动一个 nginx 然后将指定 Host 的请求转发到服务端，从而实现某一个端口 ss 服务与其他服务的共存  
在 nginx 配置文件中增加一个 server 块即可实现将所有 Host 字段为 www.google.com 的请求转发到 9377 端口       
```
server {
	listen 80;
	listen [::]:80;
	server_name  www.google.com;

	location / {
		expires off;
		proxy_buffering off;
		proxy_request_buffering off;
		proxy_http_version 1.1;
		proxy_pass http://localhost:9377/;
	}
}

```

2.实现 HTTP/HTTPS 流量的反向代理  
编辑配置文件如下，其中 localaddrs 是本地监听的地址，因为 HTTPS/HTTP 流量默认端口分别为 443/80  
mitm 选项为 true 后程序会读取到达这两个端口的流量，尝试解析 HTTPS/HTTP 请求，解析出目的地址后，开始进行反代  
```
{
  "localaddrs": ["127.0.0.1:443", "127.0.0.1:80"],
  "remoteaddr": "xx:xx:xx:xx:xx",
  "method": "chacha20",
  "partenchttps": true,
  "password": "password",
  "mitm": true
}
```
编辑 Hosts 文件，将需要代理的地址的域名解析到程序监听的 ip 地址，在这里是 127.0.0.1  
```
$ cat /etc/hosts
##
# Host Database
#
# localhost is used to configure the loopback interface
# when the system is booting.  Do not change this entry.
##
127.0.0.1	localhost
255.255.255.255	broadcasthost
::1             localhost
127.0.0.1 www.google.com
```
使用配置文件运行程序后用 curl 验证工作是否正常   
```
$ curl http://www.google.com
<HTML><HEAD><meta http-equiv="content-type" content="text/html;charset=utf-8">
<TITLE>302 Moved</TITLE></HEAD><BODY>
<H1>302 Moved</H1>
The document has moved
<A HREF="http://www.google.co.jp/?gfe_rd=cr&amp;dcr=0&amp;ei=VOXpWZmLBJDR8gfu7ZnoCg">here</A>.
</BODY></HTML>

$ curl https://www.google.com
<HTML><HEAD><meta http-equiv="content-type" content="text/html;charset=utf-8">
<TITLE>302 Moved</TITLE></HEAD><BODY>
<H1>302 Moved</H1>
The document has moved
<A HREF="https://www.google.co.jp/?gfe_rd=cr&amp;dcr=0&amp;ei=XuXpWd_fH4XR8geBqZ7QDA">here</A>.
</BODY></HTML>

$ sudo ./shadowsocks-go mitm.json
[info] [local-127.0.0.1:443] 2017/10/20 19:59:58.796889 main.go:144: run client at 127.0.0.1:443 with method chacha20
[info] [local-127.0.0.1:443] 2017/10/20 20:00:19.806849 local.go:33: proxy www.google.com:80 from 127.0.0.1:64593 -> 127.0.0.1:80 to 172.24.152.239:64594 -> xx:xx:xx:xx:xx
[info] [local-127.0.0.1:443] 2017/10/20 20:00:30.091196 local.go:33: proxy www.google.com:443 from 127.0.0.1:64607 -> 127.0.0.1:443 to 172.24.152.239:64608 -> xx:xx:xx:xx:xx
```

TODO  
____  

* ~~实现 UDP over TCP~~  
* ~~实现 UDP 隧道(用于转发 DNS 请求)~~  
* ~~实现 TCP redirect~~  
* ~~实现 HTTP 伪装~~  
* ~~实现 mux~~  
* ~~兼容 shadowsocks simple-obfs:http~~
* ~~支持 HTTP 代理~~  
* ~~支持 socks4/socks4a 代理~~
* ~~兼容 shadowsocks/simple-obfs:tls~~
