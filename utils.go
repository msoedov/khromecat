package main

import (
	"context"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"
)

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func checkErr(err error) {
	if err != nil {
		if err == context.DeadlineExceeded {
			log.Errorln("Timeout exceeded")
		} else {
			log.Errorln(err)
		}
		os.Exit(1)
	}
}

func streamFiles(dir string) {
	fs := http.FileServer(http.Dir(dir))
	http.Handle("/", fs)
	log.Println("Listening...")
	http.ListenAndServe(":3099", nil)
}

func scanMedia(dir string) []string {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil
	}
	medias := []string{}
	for _, file := range files {
		name := file.Name()
		if strings.HasSuffix(name, ".mp3") {
			log.Println("Discovered", name)
			medias = append(medias, name)
		}
	}
	return medias
}
