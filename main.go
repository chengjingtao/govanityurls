package main

import (
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"os"
	"os/signal"
	"syscall"

	"context"

	"sync"

	"gopkg.in/yaml.v2"
)

var host string
var path string
var interval time.Duration

var mutex sync.RWMutex = sync.RWMutex{}

var m map[string]struct {
	Repo    string `yaml:"repo,omitempty"`
	Display string `yaml:"display,omitempty"`
}

func init() {
	flag.StringVar(&host, "host", "", "custom domain name, e.g. alauda.cn")
	flag.StringVar(&path, "config", "/app/config/vanity.yaml", "config path, e.g. /app/config/vanity.yaml or https://example.com/vanity.yaml")
	flag.DurationVar(&interval, "interval", 2*time.Minute, "interval to refresh yaml")
}

func main() {
	flag.Parse()

	refreshWhenSig()
	refreshYaml()

	if host == "" {
		usage()
		return
	}

	http.Handle("/", http.HandlerFunc(handle))
	log.Fatalln(http.ListenAndServe("0.0.0.0:80", nil))
}

func loadYaml() error {
	log.Println("refresh yaml...")
	vanity, err := readFile()
	if err != nil {
		return err
	}

	mutex.Lock()
	defer mutex.Unlock()
	if err := yaml.Unmarshal(vanity, &m); err != nil {
		return err
	}
	for _, e := range m {
		if e.Display != "" {
			continue
		}
		if strings.Contains(e.Repo, "github.com") {
			e.Display = fmt.Sprintf("%v %v/tree/master{/dir} %v/blob/master{/dir}/{file}#L{line}", e.Repo, e.Repo, e.Repo)
		}
	}
	return nil
}

func handle(w http.ResponseWriter, r *http.Request) {
	current := r.URL.Path
	log.Printf("GET %s", current)

	mutex.RLock()
	p, ok := m[current]
	mutex.RUnlock()

	if !ok {
		log.Printf("GET 404 %s", current)
		http.NotFound(w, r)
		return
	}

	err := vanityTmpl.Execute(w, struct {
		Import  string
		Repo    string
		Display string
	}{
		Import:  host + current,
		Repo:    p.Repo,
		Display: p.Display,
	})

	if err != nil {
		http.Error(w, "cannot render the page", http.StatusInternalServerError)
		return
	}

	log.Printf("GET 200 %s", current)
}

var vanityTmpl, _ = template.New("vanity").Parse(`<!DOCTYPE html>
<html>
<head>
<meta http-equiv="Content-Type" content="text/html; charset=utf-8"/>
<meta name="go-import" content="{{.Import}} git {{.Repo}}">
<meta name="go-source" content="{{.Import}} {{.Display}}">
<meta http-equiv="refresh" content="0; url=https://godoc.org/{{.Import}}">
</head>
<body>
Nothing to see here; <a href="https://godoc.org/{{.Import}}">see the package on godoc</a>.
</body>
</html>`)

func usage() {
	fmt.Println("govanityurls is a service that allows you to set custom import paths for your go packages\n")
	fmt.Println("Usage:")
	fmt.Println("\t govanityurls -host [HOST_NAME]\n")
	flag.PrintDefaults()
}

func refreshWhenSig() {

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP)

	go func() {
		for {
			s := <-sigs
			log.Println("get signal:", s)
			loadYaml()
		}
	}()
}

func readFile() ([]byte, error) {
	if strings.HasPrefix(path, "http") || strings.HasPrefix(path, "https") {
		return loadHTTPFile()
	}
	return loadDiskFile()
}

func refreshYaml() {
	go func() {
		for {
			loadYaml()
			time.Sleep(interval)
		}
	}()
}

func loadHTTPFile() ([]byte, error) {
	http.DefaultClient.Timeout = 30
	request, err := http.NewRequestWithContext(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Printf("error to request %s: %s", path, err.Error())
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("response status code %d", resp.StatusCode)
	}

	vanity, err := ioutil.ReadAll(resp.Body)
	log.Printf("error read response: %s", err.Error())
	return vanity, err
}

func loadDiskFile() ([]byte, error) {
	vanity, err := ioutil.ReadFile(path)
	return vanity, err
}
