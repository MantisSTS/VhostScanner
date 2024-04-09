
package main

import (
	"bufio"
	"context"
	"crypto/tls"
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
	Host string
	IP   string
	ResponseHeaders []map[string]string
	ResponseBody    string
	ResponseStatus string
	Title string
}

var (
	finalResults []Results
)

func checkVHost(dialer *net.Dialer, s string, i string, v *bool, wg *sync.WaitGroup) {
	defer wg.Done()

	conn, err := tls.DialWithDialer(dialer, "tcp", i+":443", &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         s,
	})

	if err != nil {
		if *v {
			log.Printf("Could not connect to %s: %v\n", s, err)
		}
		return
	}
	defer conn.Close()

	http.DefaultTransport.(*http.Transport).DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == s+":443" {
			addr = i + ":443"
		}
		return dialer.DialContext(ctx, network, addr)
	}

	// Perform http request
	httpReq, err := http.NewRequest("GET", "https://"+s+"/", nil)
	if err != nil {
		if *v {
			log.Printf("Could not create request: %v", err)
		}
	}

	httpReq.Host = s

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		if *v {
			log.Printf("Could not send request: %v", err)
		}
	} else {
		defer resp.Body.Close()

		body, err := io.ReadAll(io.Reader(resp.Body))
		if err != nil {
			if *v {
				log.Printf("Could not read response: %v", err)
			}
		}

		if body != nil {
			// color.Green("VHost Found! [Host: %s | IP: %s]\n", s, i)
			// Check if the domain found in the SAN resolves to an IP address using DNS
			_, err = net.LookupIP(s)
			if err != nil {
				color.Green("Interesting Vhost: %s: %s\n", s, i)
				

				// Print all the HTTP response headers
				fmt.Println("-------------")
				// Print the response status
				fmt.Printf("Status: %s\n", resp.Status)

				// Print the response body title
				title := ""
				if strings.Contains(string(body), "<title>") {
					start := strings.Index(string(body), "<title>")
					end := strings.Index(string(body), "</title>")
					title = string(body)[start+len("<title>") : end]
				}
				fmt.Printf("Title: %s\n", title)
				var respHeaders []map[string]string
				// Print the response headers
				for k, v := range resp.Header {
					fmt.Printf("%s: %s\n", k, v)
					respHeaders = append(respHeaders, map[string]string{k: strings.Join(v, " ")})
				}
				fmt.Println("-------------")

				// Append to finalResults
				finalResults = append(finalResults, Results{Host: s, IP: i, ResponseHeaders: respHeaders, ResponseBody: string(body), Title: title, ResponseStatus: resp.Status})
			}
		}
	}
}

func main() {
	f := flag.String("f", "", "File to read from")
	v := flag.Bool("v", false, "Show verbose errors")
	flag.Parse()

	if *f == "" {
		log.Fatal("No file specified")
	}

	file, err := os.Open(*f)
	if err != nil {
		log.Fatal(err)
	}

	defer file.Close()

	var tlsxResults []TLSxResults

	scanner := bufio.NewScanner(file)

	// var results []Results
	for scanner.Scan() {

		line := scanner.Text()
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		// Split the line by space
		parts := strings.Split(line, " ")
		if len(parts) < 2 {
			continue
		}

		san := strings.Trim(parts[1], "[]")
		if len(tlsxResults) == 0 {
			var t TLSxResults
			t.Host = parts[0]
			t.SAN = append(t.SAN, san)
			tlsxResults = append(tlsxResults, t)
			continue
		}

		// Check if the host already exists in the slice
		found := false
		for i, t := range tlsxResults {
			if t.Host == parts[0] {
				found = true
				// Check if the SAN already exists for the host, if not add it
				sanFound := false
				for _, existingSAN := range t.SAN {
					if existingSAN == san {
						sanFound = true
						break
					}
				}
				if !sanFound {
					tlsxResults[i].SAN = append(tlsxResults[i].SAN, san)
				}
				break
			}
		}

		// If the host was not found in the slice, add a new entry
		if !found {
			var t TLSxResults
			t.Host = parts[0]
			t.SAN = append(t.SAN, san)
			tlsxResults = append(tlsxResults, t)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup

	for _, t := range tlsxResults {

		host := strings.Replace(t.Host, "https://", "", -1)
		host = strings.Replace(t.Host, ":443", "", -1)
		ip, err := net.LookupIP(host)

		if err != nil {
			if *v {
				log.Printf("Could not resolve IP for %s: %v\n", t.Host, err)
			}
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
				go checkVHost(dialer, s, i, v, &wg)
			}
		}
	}
	wg.Wait()

	writeFilename := fmt.Sprintf("vhosts_%s.txt", time.Now().Format("2006-01-02_15-04-05"))

	// Write the results to a file in the format of IP: Host like the /etc/hosts file
	fh, err := os.Create(writeFilename)
	if err != nil {
		log.Fatal(err)
	}
	defer fh.Close()
	fh.WriteString("##### Interesting Vhosts ##### \n")
	for _, r := range finalResults {
		var data string
		// Print all the HTTP response headers
		data += fmt.Sprintf("-------------\n")
		// Print the response status
		data += fmt.Sprintf("Status: %s\n", r.ResponseStatus)
		data += fmt.Sprintf("Title: %s\n", r.Title)
		// Print the response headers
		for _, v := range r.ResponseHeaders {
			for k, x := range v {
				data += fmt.Sprintf("%s: %s\n", k, x)
			}
		}
		data += fmt.Sprintf("-------------")
		
		_, err := fh.WriteString(r.IP + " " + r.Host + "\n" + data  + "\n")

		if err != nil {  
			log.Fatal(err)
		}
	}

	color.Green("Results written to %s\n", writeFilename)
}

//func removeDuplicates(elements []Results) []Results {
	// Use map to record duplicates as we find them.
//	encountered := map[Results]bool{}
//	result := []Results{}
//
/*	for v := range elements {
		if encountered[elements[v]] {
			// Do not add duplicate.
		} else {
			// Record this element as an encountered element.
			encountered[elements[v]] = true
			// Append to result slice.
			result = append(result, elements[v])
		}
	}
	// Return the new slice.
	return result
}
*/
