package main

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/barnybug/go-cast"
	"github.com/barnybug/go-cast/controllers"
	"github.com/barnybug/go-cast/discovery"
	"github.com/barnybug/go-cast/events"
	"github.com/barnybug/go-cast/log"
	"github.com/urfave/cli"
)

// Opts documents here
type Opts struct {
	Host       net.IP
	Port       int
	FileServer string
	WorkingDir string
	Timeout    time.Duration
	MediaSrcs  []string
}

func (self *Opts) init() *Opts {
	self.FileServer = ":3099"
	return self
}

func NewOpts() *Opts {
	return new(Opts).init()
}

func checkErr(err error) {
	if err != nil {
		if err == context.DeadlineExceeded {
			fmt.Println("Timeout exceeded")
		} else {
			fmt.Println(err)
		}
		os.Exit(1)
	}
}

func connect(ctx context.Context, opts *Opts) *cast.Client {

	client := cast.NewClient(opts.Host, opts.Port)

	checkErr(ctx.Err())

	fmt.Printf("Connecting to %s:%d...\n", client.IP(), client.Port())
	err := client.Connect(ctx)
	checkErr(err)
	fmt.Println("Connected")
	return client
}

func discover(timeout time.Duration) *Opts {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	discover := discovery.NewService(ctx)
	ch := make(chan *Opts)
	go func() {
		found := map[string]bool{}
		for client := range discover.Found() {
			if _, ok := found[client.Uuid()]; !ok {
				fmt.Printf("Found: %s:%d '%s' (%s) %s\n", client.IP(), client.Port(), client.Name(), client.Device(), client.Status())
				found[client.Uuid()] = true
				ch <- &Opts{Host: client.IP(), Port: client.Port(), FileServer: ":3099"}
				cancel()
				break
			}
		}
		fmt.Printf("found %#v\n", found)
	}()

	fmt.Printf("Running discovery for %s...\n", timeout)
	err := discover.Run(ctx, timeout)
	select {
	case x := <-ch:
		return x
	default:
		panic("Nothing discovered")
	}
	checkErr(err)
	return nil
}

func exposeFiles(opts *Opts) {
	fs := http.FileServer(http.Dir(opts.WorkingDir))
	http.Handle("/", fs)
	log.Println("Listening...")
	http.ListenAndServe(":3099", nil)
}

func getMedia(opts *Opts) []string {
	files, err := ioutil.ReadDir(opts.WorkingDir)
	if err != nil {
		return nil
	}
	medias := []string{}
	for _, file := range files {
		name := file.Name()
		fmt.Println(name)

		if strings.HasSuffix(name, ".mp3") {
			medias = append(medias, name)
		}
	}
	return medias
}

func play(opts *Opts) {
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	client := connect(ctx, opts)
	media, err := client.Media(ctx)
	checkErr(err)
	rand.Seed(time.Now().Unix()) // initialize global pseudo random generator
	src := opts.MediaSrcs[rand.Intn(len(opts.MediaSrcs))]
	url := fmt.Sprintf("http://%s%s/%s", GetLocalIP(), opts.FileServer, src)
	fmt.Printf("url %#v\n", url)
	contentType := "audio/mpeg"
	item := controllers.MediaItem{
		ContentId:   url,
		StreamType:  "BUFFERED",
		ContentType: contentType,
	}
	_, err = media.LoadMedia(ctx, item, 0, true, map[string]interface{}{})
	checkErr(err)
}

func discoverCommand(c *cli.Context) {
	log.Debug = c.GlobalBool("debug")
	timeout := c.GlobalDuration("timeout")
	opts := discover(timeout)
	opts.WorkingDir = c.GlobalString("dir")
	opts.Timeout = timeout
	go exposeFiles(opts)
	fmt.Printf("opts %#v\n", opts)
	opts.MediaSrcs = getMedia(opts)
	fmt.Printf("getMedia(dir) %#v\n", opts.MediaSrcs)
	play(opts)
	fmt.Println("Done")
	watchCommand(c, opts)
}

func watchCommand(c *cli.Context, opts *Opts) {
	log.Debug = c.GlobalBool("debug")
	timeout := c.GlobalDuration("timeout")

CONNECT:
	for {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		client := connect(ctx, opts)
		client.Media(ctx)
		cancel()

		for event := range client.Events {
			switch t := event.(type) {
			case events.Connected:
			case events.AppStarted:
				fmt.Printf("App started: %s [%s]\n", t.DisplayName, t.AppID)
			case events.AppStopped:
				fmt.Printf("App stopped: %s [%s]\n", t.DisplayName, t.AppID)
			case events.StatusUpdated:
				fmt.Printf("Status updated: volume %.2f [%v]\n", t.Level, t.Muted)
			case events.Disconnected:
				fmt.Printf("Disconnected: %s\n", t.Reason)
				fmt.Println("Reconnecting...")
				client.Close()
				continue CONNECT
			case controllers.MediaStatus:
				fmt.Printf("Media Status: state: %s %.1fs\n", t.PlayerState, t.CurrentTime)

				if t.PlayerState == "PAUSED" {
					return
				}
				if t.PlayerState == "IDLE" {
					play(opts)
					return
				}
			default:
				fmt.Printf("Unknown event: %#v\n", t)
			}
		}
	}
}

func GetLocalIP() string {
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
			Name:  "name",
			Usage: "chromecast name (required)",
		},
		cli.StringFlag{
			Name:  "dir",
			Usage: "directory",
			Value: ".",
		},
		cli.DurationFlag{
			Name:  "timeout",
			Value: 3 * time.Second,
		},
	}
	app := cli.NewApp()
	app.Name = "cast"
	app.Usage = "Command line tool for the Chromecast"
	app.Version = cast.Version
	app.Flags = commonFlags
	app.Commands = []cli.Command{
		{
			Name:   "d",
			Usage:  "Discover Chromecast devices",
			Action: discoverCommand,
		},
	}
	app.Run(os.Args)
	log.Println("Done")
}
