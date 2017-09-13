package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

func main() {
	// Parse command line
	var (
		dnsType      string
		gdnsCertFile string
	)

	flag.StringVar(&dnsType, "dns-type", "google", "What DNS provider")
	flag.StringVar(&gdnsCertFile, "google-credentials", "", "Google credentials file")
	flag.Parse()

	if dnsType == "" {
		log.Fatal("The dns-type command line parameter must be specified")
	}

	if dnsType == "google" {
		if gdnsCertFile == "" {
			log.Fatal("The google-credentials command line parameter must be specified when using Google DNS")
		}
	}

	log.Println("Starting Kubernetes DNS Controller...")

	var dns Interface
	var err error
	switch dnsType {
	case "google":
		dns, err = NewGoogleDNS(gdnsCertFile)
	default:
		log.Fatalf("DNS type not supported: %s", dnsType)
	}
	if err != nil {
		log.Fatal("Failed to connect to DNS provider: ", err)
	}

	// Asynchronously start watching and refreshing certs
	wg := &sync.WaitGroup{}
	done := make(chan bool)
	i := NewIngressProcessor(wg, done)

	go func() {
		for {
			select {
			case event := <-i.Events:
				log.Println(event.Type, event.Object.Name)
				if event.Type == "ADDED" || event.Type == "MODIFIED" {
					var ips []string
					for _, ingress := range event.Object.Status.LoadBalancer.Ingress {
						ips = append(ips, ingress.IP)
					}
					if len(ips) > 0 {
						for _, rule := range event.Object.Spec.Rules {
							log.Println(rule.Host, "=>", ips)
							err = dns.Update(rule.Host, ips, "A", 300)
							if err != nil {
								log.Println("Error: ", err)
							}
						}
					}
				} else if event.Type == "DELETED" {
					for _, rule := range event.Object.Spec.Rules {
						log.Println("Delete host", rule.Host)
						err = dns.Delete(rule.Host, "A")
						if err != nil {
							log.Println("Error: ", err)
						}
					}
				}
			case <-done:
				wg.Done()
				return
			}
		}
	}()
	wg.Add(1)

	s := NewServiceProcessor(wg, done)
	go func() {
		for {
			select {
			case event := <-s.Events:
				log.Println(event.Type, event.Object.Name)
				_, ok := event.Object.Annotations["k8s.brickchain.com/dns"]
				if ok {
					hostnames, ok := event.Object.Annotations["k8s.brickchain.com/dns-hostnames"]
					if ok {
						if event.Type == "ADDED" || event.Type == "MODIFIED" {
							var ips []string
							for _, ingress := range event.Object.Status.LoadBalancer.Ingress {
								ips = append(ips, ingress.IP)
							}
							if len(ips) > 0 {
								for _, hostname := range strings.Split(hostnames, ",") {
									log.Println(hostname, "=>", ips)
									err = dns.Update(hostname, ips, "A", 300)
									if err != nil {
										log.Println("Error: ", err)
									}
								}
							}
						} else if event.Type == "DELETED" {
							for _, hostname := range strings.Split(hostnames, ",") {
								log.Println("Removing", hostname)
								err = dns.Delete(hostname, "A")
								if err != nil {
									log.Println("Error: ", err)
								}
							}
						}
					}
				}
			case <-done:
				wg.Done()
				return
			}
		}
	}()
	wg.Add(1)

	log.Println("Kubernetes DNS Controller started successfully.")

	// Listen for shutdown signals
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan
	log.Println("Shutdown signal received, exiting...")
	close(done)
	wg.Wait()
	return
}
