package main

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"time"

	"golang.org/x/net/context"

	cast "github.com/barnybug/go-cast"
	"github.com/barnybug/go-cast/controllers"
	"github.com/barnybug/go-cast/discovery"
	"github.com/barnybug/go-cast/events"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

//  Grouped in type (...)
type (
	Ctrl struct {
		Opts *Opts
		Cli  *cli.Context
	}
	Opts struct {
		Host       net.IP
		Port       int
		FileServer string
		WorkingDir string
		Url        string
		Timeout    time.Duration
		MediaSrcs  []string
	}
)

func (self *Ctrl) init(c *cli.Context) *Ctrl {
	self.Opts = NewOpts()
	self.Opts.Timeout = c.GlobalDuration("timeout")
	self.Opts.WorkingDir = c.GlobalString("dir")
	self.Opts.Url = c.GlobalString("url")
	self.Cli = c
	return self
}

func NewCtrl(c *cli.Context) *Ctrl {
	return new(Ctrl).init(c)
}

func (self *Opts) init() *Opts {
	self.FileServer = ":3099"
	return self
}

func NewOpts() *Opts {
	return new(Opts).init()
}

func (self *Ctrl) connect(ctx context.Context) *cast.Client {
	var err error
	for i := 0; i < 5; i++ {
		client := cast.NewClient(self.Opts.Host, self.Opts.Port)
		checkErr(ctx.Err())

		fmt.Printf("Connecting to %s:%d...\n", client.IP(), client.Port())
		err = client.Connect(ctx)
		if err == nil {
			log.Println("Connected")
			return client
		}
		time.Sleep(time.Second)
		log.Println("Retry")
	}
	checkErr(err)
	return nil
}

func (self *Ctrl) discover() {
	ctx, cancel := context.WithTimeout(context.Background(), self.Opts.Timeout)
	defer cancel()
	discover := discovery.NewService(ctx)
	ch := make(chan *Opts)
	go func() {
		for client := range discover.Found() {
			log.Printf("Found: %s:%d '%s' (%s) %s\n", client.IP(), client.Port(), client.Name(), client.Device(), client.Status())
			self.Opts.Host = client.IP()
			self.Opts.Port = client.Port()
			cancel()
			ch <- self.Opts
			break
		}
	}()

	fmt.Printf("Running discovery for %s...\n", self.Opts.Timeout)
	discover.Run(ctx, self.Opts.Timeout)
	select {
	case <-ch:
		return
	default:
		panic("Nothing discovered")
	}
	return
}

func (self *Ctrl) chooseSrc() string {
	src := self.Opts.MediaSrcs[rand.Intn(len(self.Opts.MediaSrcs))]
	return fmt.Sprintf("http://%s%s/%s", getLocalIP(), self.Opts.FileServer, src)
}

func (self *Ctrl) play(url string) {
	ctx, cancel := context.WithTimeout(context.Background(), self.Opts.Timeout)
	defer cancel()
	client := self.connect(ctx)
	media, err := client.Media(ctx)
	checkErr(err)
	rand.Seed(time.Now().Unix()) // initialize global pseudo random generator
	fmt.Printf("url %#v\n", url)
	contentType := "audio/mpeg"
	item := controllers.MediaItem{
		ContentId:   url,
		StreamType:  "BUFFERED",
		ContentType: contentType,
	}
	_, err = media.LoadMedia(ctx, item, 0, true, map[string]interface{}{})
	if err != nil {
		log.Errorf("Ouch %s", err)
		self.play(url)
	}
}

func discoverCommand(c *cli.Context) {
	ctrl := NewCtrl(c)
	ctrl.discover()
	go streamFiles(ctrl.Opts.WorkingDir)
	log.Printf("opts %#v\n", ctrl.Opts)
	ctrl.Opts.MediaSrcs = scanMedia(ctrl.Opts.WorkingDir)
	log.Printf("scanMedia(dir) %#v\n", ctrl.Opts)
	for {
		src := ctrl.chooseSrc()
		ctrl.play(src)
		log.Println("Watching")
		time.Sleep(10)
		ctrl.watchCommand()
	}

}

func audioCommand(c *cli.Context) {
	ctrl := NewCtrl(c)
	ctrl.discover()
	url := ctrl.Opts.Url
	log.Printf("opts %#v\n", ctrl.Opts)
	ctrl.play(url)
	ctrl.watchCommand()

}

func (self *Ctrl) watchCommand() {

CONNECT:
	for {
		ctx, cancel := context.WithTimeout(context.Background(), self.Opts.Timeout)
		client := self.connect(ctx)
		client.Media(ctx)
		cancel()

		for event := range client.Events {
			switch t := event.(type) {
			case events.Connected:
			case events.AppStarted:
				log.Printf("App started: %s [%s]\n", t.DisplayName, t.AppID)
			case events.AppStopped:
				log.Printf("App stopped: %s [%s]\n", t.DisplayName, t.AppID)
			case events.StatusUpdated:
				log.Printf("Status updated: volume %.2f [%v]\n", t.Level, t.Muted)
			case events.Disconnected:
				log.Printf("Disconnected: %s\n", t.Reason)
				log.Println("Reconnecting...")
				client.Close()
				continue CONNECT
			case controllers.MediaStatus:
				log.Printf("Media Status: state: %s %.1fs\n", t.PlayerState, t.CurrentTime)

				if t.PlayerState == "PAUSED" {
					os.Exit(0)
					return
				}
				if t.PlayerState == "PLAYING" && t.CurrentTime > 10 {
					log.Println("Playing next...")
					return

				}

				if t.PlayerState == "IDLE" {
					log.Println("Playing next idle ...")
					return
				}
			default:
				log.Printf("Unknown event: %#v\n", t)
			}
		}
	}
}

func main() {
	commonFlags := []cli.Flag{
		cli.StringFlag{
			Name:  "host",
			Usage: "chromecast hostname or IP (required)",
		},
		cli.IntFlag{
			Name:  "port",
			Usage: "chromecast port",
			Value: 8009,
		},
		cli.StringFlag{
			Name:  "dir",
			Usage: "directory",
			Value: ".",
		},
		cli.StringFlag{
			Name:  "url",
			Usage: "url",
			Value: "http://107.182.230.196/audio1110/2.mp3",
		},
		cli.DurationFlag{
			Name:  "timeout",
			Value: 5 * time.Second,
		},
	}
	app := cli.NewApp()
	app.Name = "khromecat"
	app.Usage = "Command line tool for the Chromecast"
	app.Version = cast.Version
	app.Flags = commonFlags
	app.Commands = []cli.Command{
		{
			Name:   "d",
			Usage:  "Play from the local directory",
			Action: discoverCommand,
		},
		{
			Name:   "a",
			Usage:  "Play from the internet",
			Action: audioCommand,
		},
	}
	app.Run(os.Args)
	log.Println("Done")
}
