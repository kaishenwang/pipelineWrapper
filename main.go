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
)

var (
	urlFile	string
	zmapLocation  string
	zdnsOutputFile string
	logFile   string

	domainToUrl map[string][]string
	domains strings.Builder
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
		//fmt.Println(fqdn)
		if !ok {
			domains.WriteString(fqdn + "\n")
		}

	}
	return nil
}


func main() {
	flags := flag.NewFlagSet("flags", flag.ExitOnError)
	flags.StringVar(&urlFile, "url-file", "-", "file contains all urls")
	flags.StringVar(&zmapLocation, "zmap-location", "", "location of zmap binary")
	flags.StringVar(&zdnsOutputFile, "zdns-output-file", "RR.json", "file for original output of zdns")
	flags.StringVar(&logFile, "log-file", "", "file for log")
	flags.Parse(os.Args[1:])

	var readUrlWG sync.WaitGroup
	readUrlWG.Add(1)
	go readURL(&readUrlWG)
	readUrlWG.Wait()
	
	exeZDNS := exec.Command(os.Getenv("GOPATH")+"/src/github.com/kwang40/zdns/zdns/./zdns", "ALOOKUP", "-iterative", "--cache-size=500000", "--std-out-modules=A", "--input-file=tmp.txt", "--output-file="+zdnsOutputFile)
	exeZDNS.Stdin = strings.NewReader(domains.String())
	exeZDNS.Stdout = os.Stdout
	exeZDNS.Stderr= os.Stderr

	if err := exeZDNS.Start(); err != nil { //Use start, not run
		fmt.Println("An error occured: ", err) //replace with logger, or anything you want
	}

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
