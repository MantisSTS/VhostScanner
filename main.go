package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

type TLSxResults struct {
	Host string
	IP   []string
	SAN  []string
}

type Results struct {
	ResponseStatus  string `json:"ResponseStatus,omitempty"`
	Host            string `json:"Host"`
	IP              string `json:"IP"`
	Title           string `json:"Title"`
	ResponseHeaders []string
	ResponseBody    string `json:"ResponseBody,omitempty"`
}

type Flags struct {
	file        string
	verbose     bool
	includeBody bool
}

var (
	finalResults sync.Map
	flags        Flags
	clientPool   sync.Pool
)

func checkVHost(dialer *net.Dialer, s string, i string, wg *sync.WaitGroup) {
	defer wg.Done()

	conn, err := tls.DialWithDialer(dialer, "tcp", i+":443", &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         s,
	})

	if err != nil {
		if flags.verbose {
			log.Printf("Could not connect to %s: %v\n", s, err)
		}
		return
	}
	defer conn.Close()

	client := clientPool.Get().(*http.Client)
	client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == s+":443" {
			addr = i + ":443"
		}
		return dialer.DialContext(ctx, network, addr)
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), "GET", "https://"+s+"/", nil)
	if err != nil {
		if flags.verbose {
			log.Printf("Could not create request: %v", err)
		}
		return
	}

	httpReq.Host = s

	resp, err := client.Do(httpReq)
	if err != nil {
		if flags.verbose {
			log.Printf("Could not send request: %v", err)
		}
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if flags.verbose {
			log.Printf("Could not read response: %v", err)
		}
		return
	}

	if body != nil {
		_, err = net.LookupIP(s)
		if err != nil {
			color.Green("Interesting Vhost: %s: %s\n", s, i)

			title := ""
			if strings.Contains(string(body), "<title>") {
				start := strings.Index(string(body), "<title>")
				end := strings.Index(string(body), "</title>")
				title = string(body)[start+len("<title>") : end]
			}

			var respHeaders []string
			for k, v := range resp.Header {
				respHeaders = append(respHeaders, fmt.Sprintf("%s: %s", k, strings.Join(v, " ")))
			}

			result := Results{
				ResponseStatus:  resp.Status,
				Host:            s,
				IP:              i,
				Title:           title,
				ResponseHeaders: respHeaders,
			}

			if flags.includeBody {
				result.ResponseBody = string(body)
			}

			finalResults.Store(s, result)
		}
	}
	clientPool.Put(client)
}

func main() {
	// Todo: add concurrency flag
	flag.StringVar(&flags.file, "f", "", "File to read from")
	flag.BoolVar(&flags.verbose, "v", false, "Show verbose errors")
	flag.BoolVar(&flags.includeBody, "b", false, "Include the Body of the response in the output")
	flag.Parse()

	if flags.file == "" {
		log.Fatal("No file specified")
	}

	file, err := os.Open(flags.file)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	var tlsxResults []TLSxResults
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Split(line, " ")
		if len(parts) < 2 {
			continue
		}

		san := strings.Trim(parts[1], "[]")
		if len(tlsxResults) == 0 {
			tlsxResults = append(tlsxResults, TLSxResults{
				Host: parts[0],
				SAN:  []string{san},
			})
			continue
		}

		found := false
		for i, t := range tlsxResults {
			if t.Host == parts[0] {
				found = true
				for _, existingSAN := range t.SAN {
					if existingSAN == san {
						found = true
						break
					}
				}
				if !found {
					tlsxResults[i].SAN = append(tlsxResults[i].SAN, san)
				}
				break
			}
		}

		if !found {
			tlsxResults = append(tlsxResults, TLSxResults{
				Host: parts[0],
				SAN:  []string{san},
			})
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup

	for _, t := range tlsxResults {
		host := strings.ReplaceAll(strings.ReplaceAll(t.Host, "https://", ""), ":443", "")
		ip, err := net.LookupIP(host)
		if err != nil {
			if flags.verbose {
				log.Printf("Could not resolve IP for %s: %v\n", t.Host, err)
			}
			continue
		}

		for _, i := range ip {
			t.IP = append(t.IP, i.String())
		}

		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}

		for _, s := range t.SAN {
			for _, i := range t.IP {
				wg.Add(1)
				// Todo: use concurrency flag for how many goroutines to run
				go checkVHost(dialer, s, i, &wg)
			}
		}
	}

	wg.Wait()

	writeFilename := fmt.Sprintf("vhosts_%s.json", time.Now().Format("2006-01-02_15-04-05"))
	fh, err := os.Create(writeFilename)
	if err != nil {
		log.Fatal(err)
	}
	defer fh.Close()

	enc := json.NewEncoder(fh)
	finalResults.Range(func(key, value interface{}) bool {
		err := enc.Encode(value)
		if err != nil {
			log.Printf("Could not encode JSON: %v", err)
		}
		return true
	})

	if err != nil {
		log.Fatal(err)
	}

	color.Green("Results written to %s\n", writeFilename)
}

func init() {
	clientPool = sync.Pool{
		New: func() interface{} {
			return &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
				},
			}
		},
	}
}
