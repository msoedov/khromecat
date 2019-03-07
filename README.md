# khromecat

A command line tool to play local files and directories on Google Chromecast devices.

Can handle `Play next` voice command

## Installation

```
go get github.com/msoedov/khromecat
go install github.com/msoedov/khromecat

```

## Usage

Play mp3 filesfrom the current directory
```
khromecat d
```


Play internet radio link

```
khromecat --url=http://ic7.101.ru:8000/a99 a
```
