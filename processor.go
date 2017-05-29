package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/pkg/errors"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
)

const (
	apiHost            = "http://127.0.0.1:8001"
	certEndpoint       = "/apis/stable.k8s.psg.io/v1/namespaces/%s/certificates"
	certEndpointAll    = "/apis/stable.k8s.psg.io/v1/certificates"
	ingressEndpoint    = "/apis/extensions/v1beta1/namespaces/%s/ingresses"
	ingressEndpointAll = "/apis/extensions/v1beta1/ingresses"
	serviceEndpoint    = "/api/v1/namespace/%s/services"
	serviceEndpointAll = "/api/v1/services"
	secretsEndpoint    = "/api/v1/namespaces/%s/secrets"
	secretsEndpointAll = "/api/v1/secrets"
	eventsEndpoint     = "/api/v1/namespaces/%s/events"

	annotationNamespace = "k8s.brickchain.com/dns"
)

type WatchEvent struct {
	Type   string          `json:"type"`
	Object json.RawMessage `json:"object"`
}

type IngressEvent struct {
	Type   string          `json:"type"`
	Object v1beta1.Ingress `json:"object"`
}

type ServiceEvent struct {
	Type   string     `json:"type"`
	Object v1.Service `json:"object"`
}

type IngressProcessor struct {
	Events chan (IngressEvent)
	done   chan (bool)
	wg     *sync.WaitGroup
}

type ServiceProcessor struct {
	Events chan (ServiceEvent)
	done   chan (bool)
	wg     *sync.WaitGroup
}

func NewIngressProcessor(wg *sync.WaitGroup, done chan bool) *IngressProcessor {
	i := &IngressProcessor{
		Events: make(chan (IngressEvent)),
		done:   done,
		wg:     wg,
	}

	go i.run()
	i.wg.Add(1)

	return i
}

func (d *IngressProcessor) run() {
	ingressEvents, ingressErrs := monitorIngressEvents(ingressEndpointAll)
	watchErrs := make(chan error)
	go func() {
		for {
			select {
			case err := <-ingressErrs:
				watchErrs <- err
			case <-d.done:
				return
			}
		}
	}()
	for {
		select {
		case event := <-ingressEvents:
			d.processIngressEvent(event)
		case err := <-watchErrs:
			log.Printf("Error while watching kubernetes events: %v", err)
		case <-d.done:
			d.wg.Done()
			log.Println("Stopped DNS event watcher.")
			return
		}
	}
}

func (d *IngressProcessor) processIngressEvent(event IngressEvent) {
	annotation, ok := event.Object.Annotations[annotationNamespace]
	if ok {
		if annotation == "true" {
			d.Events <- event
		}
	} else {
		log.Println("Event did not have our annotation, ignoring.")
	}
}

func NewServiceProcessor(wg *sync.WaitGroup, done chan bool) *ServiceProcessor {
	i := &ServiceProcessor{
		Events: make(chan (ServiceEvent)),
		done:   done,
		wg:     wg,
	}

	go i.run()
	i.wg.Add(1)

	return i
}

func (d *ServiceProcessor) run() {
	serviceEvents, serviceErrs := monitorServiceEvents(serviceEndpointAll)
	watchErrs := make(chan error)
	go func() {
		for {
			select {
			case err := <-serviceErrs:
				watchErrs <- err
			case <-d.done:
				return
			}
		}
	}()
	for {
		select {
		case event := <-serviceEvents:
			d.processServiceEvent(event)
		case err := <-watchErrs:
			log.Printf("Error while watching kubernetes events: %v", err)
		case <-d.done:
			d.wg.Done()
			log.Println("Stopped DNS event watcher.")
			return
		}
	}
}

func (d *ServiceProcessor) processServiceEvent(event ServiceEvent) {
	annotation, ok := event.Object.Annotations[annotationNamespace]
	if ok {
		if annotation == "true" {
			d.Events <- event
		}
	} else {
		log.Println("Event did not have our annotation, ignoring.")
	}
}

func ingressReference(ing v1beta1.Ingress, path string) v1.ObjectReference {
	return v1.ObjectReference{
		Kind:            "Ingress",
		Namespace:       ing.Namespace,
		Name:            ing.Name,
		UID:             ing.UID,
		ResourceVersion: ing.ResourceVersion,
		FieldPath:       path,
	}
}

func getIngresses(endpoint string) ([]v1beta1.Ingress, error) {
	var resp *http.Response
	var err error

	for {
		resp, err = http.Get(apiHost + endpoint)
		if err != nil {
			log.Printf("Error while retrieving ingress: %v. Retrying in 5 seconds", err)
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}

	var ingressList v1beta1.IngressList
	err = json.NewDecoder(resp.Body).Decode(&ingressList)
	if err != nil {
		return nil, err
	}

	return ingressList.Items, nil
}

func monitorEvents(endpoint string) (<-chan WatchEvent, <-chan error) {
	events := make(chan WatchEvent)
	errc := make(chan error, 1)
	go func() {
		resourceVersion := "0"
		for {
			resp, err := http.Get(apiHost + endpoint + "?watch=true&resourceVersion=" + resourceVersion)
			if err != nil {
				errc <- err
				time.Sleep(5 * time.Second)
				continue
			}
			if resp.StatusCode != 200 {
				errc <- errors.New("Invalid status code: " + resp.Status)
				time.Sleep(5 * time.Second)
				continue
			}

			decoder := json.NewDecoder(resp.Body)
			for {
				var event WatchEvent
				err = decoder.Decode(&event)
				if err != nil {
					if err != io.EOF {
						errc <- err
					}
					break
				}
				var header struct {
					Metadata struct {
						ResourceVersion string `json:"resourceVersion"`
					} `json:"metadata"`
				}
				if err := json.Unmarshal([]byte(event.Object), &header); err != nil {
					errc <- err
					break
				}
				resourceVersion = header.Metadata.ResourceVersion
				events <- event
			}
		}
	}()

	return events, errc
}

func monitorIngressEvents(endpoint string) (<-chan IngressEvent, <-chan error) {
	rawEvents, rawErrc := monitorEvents(endpoint)
	events := make(chan IngressEvent)
	errc := make(chan error, 1)
	go func() {
		for {
			select {
			case ev := <-rawEvents:
				var event IngressEvent
				event.Type = ev.Type
				err := json.Unmarshal([]byte(ev.Object), &event.Object)
				if err != nil {
					errc <- err
					continue
				}
				events <- event
			case err := <-rawErrc:
				errc <- err
			}
		}
	}()

	return events, errc
}

func monitorServiceEvents(endpoint string) (<-chan ServiceEvent, <-chan error) {
	rawEvents, rawErrc := monitorEvents(endpoint)
	events := make(chan ServiceEvent)
	errc := make(chan error, 1)
	go func() {
		for {
			select {
			case ev := <-rawEvents:
				var event ServiceEvent
				event.Type = ev.Type
				err := json.Unmarshal([]byte(ev.Object), &event.Object)
				if err != nil {
					errc <- err
					continue
				}
				events <- event
			case err := <-rawErrc:
				errc <- err
			}
		}
	}()

	return events, errc
}

func namespacedEndpoint(endpoint string, namespace string) string {
	return fmt.Sprintf(endpoint, namespace)
}
