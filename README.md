# quartz

a simple, bare bones file sharing service for generating single use download links written in Go.

allows you to upload a file, and copy a link or qr code to share to someone to download. after downloaded, the file is immediately deleted and removed from the server.

## running

to use, you need to build and start the docker container via compose.

```bash
git clone https://github.com/jamiw1/quartz.git
cd quartz
```

once it's cloned, feel free to modify the docker-compose.yml to your liking. next, build and start the container.

```bash
docker compose up -d --build
```

the default port is `3000`, you can check to see if it's working at `http://localhost:3000` or whatever your server ip is

## building

first you need to download the dependencies. note: requires Go 1.26 or later.

```bash
go mod download
```

to build and run, do this

```bash
go run .
```

to just build an executable/binary, do this

windows:

```bash
go build -o quartz.exe
quartz.exe
```

linux/macos:

```bash
go build -o quartz
./quartz
```
