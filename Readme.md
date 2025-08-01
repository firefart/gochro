# gochro

gochro is a small docker image with chromium installed and a golang based webserver to interact with it. It can be used to take screenshots of websites using chromium-headless and convert HTML pages to PDF.

If errors occur the error will be logged to stdout and a non information leaking error message is presented to the user.

This project was used on [https://wpscan.io](https://wpscan.io) for taking website screenshots and to generate PDF reports.

## Screenshot

This URL takes a Screenshot of [https://firefart.at](https://firefart.at) with a resolution of 1024x768 and returns an image.

[http://localhost:8080/screenshot?url=https://firefart.at&w=1024&h=768](http://localhost:8080/screenshot?url=https://firefart.at&w=1024&h=768)

## HTML 2 PDF

Send a POST request with the HTML you want to convert in the Post body to the following url.

[http://localhost:8080/html2pdf?w=1024&h=768](http://localhost:8080/html2pdf?w=1024&h=768)

This will return a PDF of the HTML input.

Example:

```text
POST /html2pdf?w=1024&h=768 HTTP/1.1
Host: localhost:8080
Content-Type: application/x-www-form-urlencoded
Content-Length: 119

<html>
<head><title>Test Page</title></head>
<body>
<h1>This is a test</h1>
<p>This is a test</p>
</body>
</html>
```

Example as curl:

```text
curl -s -k -X 'POST' -o test.pdf --data-binary '<html><body><h1>test</h1></body></html>' 'http://127.0.0.1:8080/html2pdf'
```

## URL 2 PDF

Send a GET request to the following url to get the response as PDF.

[http://localhost:8080/url2pdf?url=https://firefart.at&w=1024&h=768](http://localhost:8080/url2pdf?url=https://firefart.at&w=1024&h=768)

## Run server

To run this image you should use the [seccomp profile](https://github.com/jessfraz/dotfiles/blob/master/etc/docker/seccomp/chrome.json) provided by [Jess Frazelle](https://github.com/jessfraz). The privileges on the host are needed for chromiums internal security sandbox. You can also deactivate the sandbox on chromium (would require changes in `main.go`) but that's a bad idea and puts your server at risk, so please use the seccomp profile instead.

Be sure to use the --init switch to get rid of zombie processes of chromium.

### Command Line Options

```text
-host                  The host and port to listen of (refers to inside the container). Defaults to 127.0.0.1:8080
-debug                 Enables debug output. Default: false
-ignore-cert-errors    Also fetch ressources from origins with untrusted certificates or cert errors.
-proxy                 Use a proxy server to connect to the internet. Please use format IP:PORT without a protocol. Example: 1.2.3.4:3128
```

### Use the docker hub image

You can also use the [prebuit image](https://hub.docker.com/r/firefart/gochro) from dockerhub.

To pull the image run

```bash
docker pull firefart/gochro
```

### Include in docker-compose

If you want to include this image in a docker-compose file you can use the following example. Just connect the `gochronet` to the other service so the containers can communicate with each other.

Please note that the `0.0.0.0` in the command only applies to the network inside the docker container itself. If you want to access it from your local machine you need to add a port mapping.

```yml
version: "3.7"

services:
  gochro:
    image: firefart/gochro
    init: true
    container_name: gochro
    security_opt:
      - seccomp="chrome.json"
    command: -host 0.0.0.0:8080
    networks:
      - gochronet

networks:
  gochronet:
    driver: bridge
```
