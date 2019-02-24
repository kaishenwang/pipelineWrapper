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
	"github.com/orcaman/concurrent-map"
	"time"
)

var (
	urlFile	string
	zmapExecutable  string
	zdnsOutputFile string
	logFile   string
	outputFile string

	domainToUrl map[string][]string
	domains strings.Builder
	ipToDomains cmap.ConcurrentMap
	ipOpen map[int32] bool
	domainSent map[string] bool

	// some var for logging
	validUrlCount, invalidUrlCount, uniqueDomainCount, totalIpCount, uniqueIpCount, uniqueOpenIpCount, domainOpenCount, urlOpenCount int32
	pipelineStart, readUrlFinished, zdnsFinished, zmapFinished, allFinished time.Time
)

func logPipelineMetrics() {
	f, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatal("unable to open output file:", err.Error())
	}
	defer f.Close()

	// log count
	f.WriteString(fmt.Sprintf("validUrlCount:%d\n",validUrlCount))
	f.WriteString(fmt.Sprintf("invalidUrlCount:%d\n",invalidUrlCount))
	f.WriteString(fmt.Sprintf("uniqueDomainCount:%d\n",uniqueDomainCount))
	f.WriteString(fmt.Sprintf("totalIpCount:%d\n",totalIpCount))
	f.WriteString(fmt.Sprintf("uniqueIpCount:%d\n",uniqueIpCount))
	f.WriteString(fmt.Sprintf("uniqueOpenIpCount:%d\n",uniqueOpenIpCount))
	f.WriteString(fmt.Sprintf("domainOpenCount:%d\n",domainOpenCount))
	f.WriteString(fmt.Sprintf("urlOpenCount:%d\n",urlOpenCount))
	// log time cost
	f.WriteString("pipelineStart:"+pipelineStart.String()+"\n")
	f.WriteString("readUrlFinished:"+readUrlFinished.String()+"\n")
	f.WriteString("zdnsFinished:"+zdnsFinished.String()+"\n")
	f.WriteString("zmapFinished:"+zmapFinished.String()+"\n")
	f.WriteString("allFinished:"+allFinished.String()+"\n")
	
}

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
			invalidUrlCount += 1
			continue
		}

		validUrlCount += 1
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
	uniqueDomainCount = int32(len(domainToUrl))
	return nil
}

func processZDNSOutput(wg *sync.WaitGroup, reader io.ReadCloser, zmapInput chan<- []byte) {
	defer (*wg).Done()
	defer close(zmapInput)
	ipToDomains = cmap.New()
	rd := bufio.NewReader(reader)
	var emptySlice [] string
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSuffix(line, "\n")
		parts := strings.Split(line, ",")
		ip := parts[0]
		domain := parts[1]
		totalIpCount += 1
		if tmp, ok := ipToDomains.Get(ip); ok {
			value := tmp.([]string)
			ipToDomains.Set(ip, append(value, domain))
		} else {
			uniqueIpCount += 1
			ipToDomains.Set(ip, append(emptySlice, domain))
		}

		zmapInput <- []byte(ip+"\n")
	}
}

func processZmapOutput (wg *sync.WaitGroup, reader io.ReadCloser) {
	defer (*wg).Done()
	var f *os.File
	if outputFile == "" || outputFile == "-" {
		f = os.Stdout
	} else {
		var err error
		f, err = os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			log.Fatal("unable to open output file:", err.Error())
		}
		defer f.Close()
	}
	ipOpen = make(map[int32]bool)
	domainSent = make(map[string]bool)
	rd := bufio.NewReader(reader)
	var ipAddr string
	var key int32
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSuffix(line, "\n")
		if len(line) > 1{
			if line[0] != '#'{
				uniqueOpenIpCount += 1
				ipAddr = line
				ipOpen[key] = true
			} else {
				ipAddr = line[1:len(line)]
				if _, ok := ipOpen[key]; !ok {
					continue
				}
			}
		} else {
			return
		}

		tmp,_ := ipToDomains.Get(ipAddr)
		domains := tmp.([]string)
		for _,domain := range(domains){
			if _,ok := domainSent[domain]; ok {
				continue
			}
			domainOpenCount += 1
			domainSent[domain] = true
			for _,url := range(domainToUrl[domain]) {
				urlOpenCount += 1
				if _, err := f.WriteString(fmt.Sprintf("%s,%s\n",ipAddr,url)); err != nil {
					log.Fatal("Problem writing", err.Error())
				}
			}

		}
	}

}

func main() {
	pipelineStart = time.Now()
	flags := flag.NewFlagSet("flags", flag.ExitOnError)
	flags.StringVar(&urlFile, "url-file", "-", "file contains all urls")
	flags.StringVar(&zmapExecutable, "zmap-excutable", "", "location of zmap binary")
	flags.StringVar(&zdnsOutputFile, "zdns-output-file", "RR.json", "file for original output of zdns")
	flags.StringVar(&outputFile, "output-file", "-", "file for output, stdout as default")
	flags.StringVar(&logFile, "log-file", "log.txt", "file for log")
	flags.Parse(os.Args[1:])

	// waitGroup
	var readUrlWG, processZDNSOutputWG, processZmapOutputWG sync.WaitGroup
	readUrlWG.Add(1)
	processZDNSOutputWG.Add(1)
	processZmapOutputWG.Add(1)

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
	readUrlFinished = time.Now()

	// commands
	exeZDNS := exec.Command(os.Getenv("GOPATH")+"/src/github.com/kwang40/zdns/zdns/./zdns", "ALOOKUP", "-iterative", "--cache-size=500000", "--std-out-modules=A", "--output-file="+zdnsOutputFile)
	exeZDNS.Stdin = strings.NewReader(domains.String())
	exeZDNSOut,_ := exeZDNS.StdoutPipe()

	exeZmap := exec.Command(zmapExecutable, "--whitelist-file=testWhiteList.txt", "--target-port=80")
	exeZmap.Stdin = chanrw.NewReader(zmapInput)
	exeZmapOut,_ := exeZmap.StdoutPipe()

	// Start all components from the end of pipeline
	// Start process final output
	go processZmapOutput(&processZmapOutputWG, exeZmapOut)
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
	zdnsFinished = time.Now()
	processZDNSOutputWG.Wait()
	if err := exeZmap.Wait(); err != nil {
		log.Fatal(err)
	}
	zmapFinished = time.Now()
	processZmapOutputWG.Wait()

	allFinished = time.Now()

	logPipelineMetrics()
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
