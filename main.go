package main

import (
	"flag"
	"os"
	"sync"
	"log"
	"bufio"
	"strings"
	"net/url"
	"os/exec"
	"fmt"
	"io"
	"github.com/kwang40/chanrw"
	"io/ioutil"
)

var (
	urlFile	string
	zmapExecutable  string
	zdnsOutputFile string
	logFile   string

	domainToUrl map[string][]string
	domains strings.Builder
	ipToDomains map[int32][]string
)


func readURL(wg *sync.WaitGroup) error {
	defer (*wg).Done()

	var f *os.File
	if urlFile == "" || urlFile == "-" {
		f = os.Stdin
	} else {
		var err error
		f, err = os.Open(urlFile)
		if err != nil {
			log.Fatal("unable to open input file:", err.Error())
		}
	}
	domainToUrl = make(map[string][]string)
	s := bufio.NewScanner(f)
	for s.Scan() {
		urlStr := s.Text()

		if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
			urlStr = "http://" + urlStr
		}

		u, err := url.Parse(urlStr)
		if err != nil {
			continue
			//log.Fatal("invalid url: ", err)
		}

		fqdn := u.Hostname()
		value := make([]string, 0)
		var ok bool
		value, ok = domainToUrl[fqdn]
		if !contains(value, urlStr) {
			domainToUrl[fqdn] = append(value, urlStr)
		}
		if !ok {
			domains.WriteString(fqdn + "\n")
		}

	}
	return nil
}

func processZDNSOutput(wg *sync.WaitGroup, reader io.ReadCloser, zmapInput chan<- []byte) {
	defer (*wg).Done()
	defer close(zmapInput)
	ipToDomains = make(map[int32][]string)
	rd := bufio.NewReader(reader)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSuffix(line, "\n")
		parts := strings.Split(line, ",")
		ip := parts[0]
		domain := parts[1]
		key := ipStr2Int(ip)
		ipToDomains[key] = append(ipToDomains[key], domain)
		zmapInput <- []byte(ip+"\n")
	}
}


func main() {
	flags := flag.NewFlagSet("flags", flag.ExitOnError)
	flags.StringVar(&urlFile, "url-file", "-", "file contains all urls")
	flags.StringVar(&zmapExecutable, "zmap-excutable", "", "location of zmap binary")
	flags.StringVar(&zdnsOutputFile, "zdns-output-file", "RR.json", "file for original output of zdns")
	flags.StringVar(&logFile, "log-file", "", "file for log")
	flags.Parse(os.Args[1:])

	// waitGroup
	var readUrlWG, processZDNSOutputWG sync.WaitGroup
	readUrlWG.Add(1)
	processZDNSOutputWG.Add(1)

	// channels
	zmapInput := make(chan []byte)

	// Parse all urls
	go readURL(&readUrlWG)
	// Create dummy whitelist file for zmap
	err := ioutil.WriteFile("testWhiteList.txt", []byte("122.227.0.14/32\n"), 0644)
	if err != nil {
		log.Fatal("An error occured: ", err)
	}
	readUrlWG.Wait()

	// commands
	exeZDNS := exec.Command(os.Getenv("GOPATH")+"/src/github.com/kwang40/zdns/zdns/./zdns", "ALOOKUP", "-iterative", "--cache-size=500000", "--std-out-modules=A", "--output-file="+zdnsOutputFile)
	exeZDNS.Stdin = strings.NewReader(domains.String())
	exeZDNSOut,_ := exeZDNS.StdoutPipe()

	exeZmap := exec.Command(zmapExecutable, "--whitelist-file=testWhiteList.txt", "--target-port=80")
	exeZmap.Stdin = chanrw.NewReader(zmapInput)
	exeZmap.Stdout = os.Stdout
	exeZmap.Stderr = os.Stderr
	//exeZmapOut,_ := exeZmap.StdoutPipe()

	// Start all components from the end of pipeline
	// Start zmap
	if err := exeZmap.Start(); err != nil {
		log.Fatal("An error occured: ", err)
	}
	// start component between zdns and zmap
	go processZDNSOutput(&processZDNSOutputWG, exeZDNSOut, zmapInput)
	// start zdns
	if err := exeZDNS.Start(); err != nil {
		log.Fatal("An error occured: ", err)
	}

	// Wait for all components from the start of pipeline
	if err := exeZDNS.Wait(); err != nil {
		log.Fatal(err)
	}
	processZDNSOutputWG.Wait()
	if err := exeZmap.Wait(); err != nil {
		log.Fatal(err)
	}
	//var testWG sync.WaitGroup
	//testWG.Add(1)

	//go testOutput(&testWG, zmapInput)







	//testWG.Wait()
	return
}

// Utilities

func contains(arr []string, str string) bool {
	for _, val := range arr {
		if str == val {
			return true
		}
	}
	return false
}

func ipStr2Int(ip string) int32{
	var n0,n1,n2,n3 int32
	fmt.Sscanf(ip, "%d.%d.%d.%d", &n0,&n1,&n2,&n3)
	return (n0<<24)|(n1<<16)|(n2<<8)|(n3)

}

func testOutput(wg *sync.WaitGroup, in <- chan string) {
	defer wg.Done()
	for line := range(in) {
		fmt.Print(line)
	}
}
