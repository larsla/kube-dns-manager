package main

import (
	"fmt"
	"io/ioutil"
	"log"

	"strings"

	"encoding/json"

	"time"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	dns "google.golang.org/api/dns/v1"
)

type GoogleDNS struct {
	projectID string
	dnsClient *dns.Service
}

func NewGoogleDNS(serviceAccountFile string) (*GoogleDNS, error) {
	ctx := context.Background()

	jsonKey, err := ioutil.ReadFile(serviceAccountFile)
	if err != nil {
		return nil, err
	}

	var jwt map[string]string
	err = json.Unmarshal(jsonKey, &jwt)
	if err != nil {
		return nil, err
	}
	projectID, ok := jwt["project_id"]
	if !ok {
		return nil, fmt.Errorf("Could not get project_id from JWT")
	}

	conf, err := google.JWTConfigFromJSON(jsonKey, dns.CloudPlatformScope, dns.NdevClouddnsReadwriteScope)
	if err != nil {
		return nil, err
	}

	ts := conf.TokenSource(ctx)
	client := oauth2.NewClient(ctx, ts)
	d, err := dns.New(client)
	if err != nil {
		return nil, err
	}

	return &GoogleDNS{
		projectID: projectID,
		dnsClient: d,
	}, nil
}

func (d *GoogleDNS) Update(name string, ip []string, recordType string, ttl int64) error {
	if !strings.HasSuffix(name, ".") {
		name = fmt.Sprintf("%s.", name)
	}

	relevantZone, err := d.findRelevantZone(name)
	if err != nil {
		return err
	}

	if relevantZone != nil {
		var existingRR *dns.ResourceRecordSet
		rrlc := d.dnsClient.ResourceRecordSets.List(d.projectID, relevantZone.Name)
		rrl, err := rrlc.Do()
		if err != nil {
			return err
		}
		for _, rr := range rrl.Rrsets {
			if rr.Name == name && rr.Type == recordType {
				existingRR = rr
			}
		}

		var rrs []*dns.ResourceRecordSet
		rrs = append(rrs, &dns.ResourceRecordSet{
			Name:    name,
			Rrdatas: ip,
			Type:    recordType,
			Ttl:     ttl,
		})
		c := dns.Change{
			Additions: rrs,
		}
		if existingRR != nil {
			c.Deletions = append(c.Deletions, existingRR)
		}

		d.executeChange(relevantZone, &c)

	} else {
		log.Println("Didn't find any relevant zone to update for", name)
	}

	return nil
}

func (d *GoogleDNS) Delete(name, recordType string) error {
	if !strings.HasSuffix(name, ".") {
		name = fmt.Sprintf("%s.", name)
	}

	zone, rr, err := d.findZoneWithHost(name)
	if err != nil {
		return err
	}

	if rr != nil {
		c := dns.Change{}
		c.Deletions = append(c.Deletions, rr)
		d.executeChange(zone, &c)

	} else {
		return fmt.Errorf("Could not find zone with host %s", name)
	}

	return nil

}

func (d *GoogleDNS) executeChange(zone *dns.ManagedZone, changeSet *dns.Change) error {
	change := d.dnsClient.Changes.Create(d.projectID, zone.Name, changeSet)
	cd, err := change.Do()
	if err != nil {
		return err
	}

	timeout := time.Duration(time.Second * 5)
	startTime := time.Now()
	for {
		if time.Now().After(startTime.Add(timeout)) {
			return fmt.Errorf("Timeout")
		}
		cdc := d.dnsClient.Changes.Get(d.projectID, zone.Name, cd.Id)
		cdci, err := cdc.Do()
		if err != nil {
			return err
		}
		if cdci.Status != "pending" {
			if cdci.Status == "done" {
				return nil
			}
			return fmt.Errorf("Expecting status to be done, not %s", cdci.Status)
		}
		time.Sleep(time.Second)
	}
}

func (d *GoogleDNS) findRelevantZone(name string) (*dns.ManagedZone, error) {
	var relevantZone *dns.ManagedZone

	zonesCall := d.dnsClient.ManagedZones.List(d.projectID)
	zones, err := zonesCall.Do()
	if err != nil {
		return nil, err
	}
	for _, zone := range zones.ManagedZones {
		if strings.HasSuffix(name, zone.DnsName) {
			if relevantZone == nil {
				relevantZone = zone
			} else {
				if len(zone.DnsName) > len(relevantZone.DnsName) {
					relevantZone = zone
				}
			}
		}
	}

	return relevantZone, nil
}

func (d *GoogleDNS) findZoneWithHost(name string) (*dns.ManagedZone, *dns.ResourceRecordSet, error) {

	zonesCall := d.dnsClient.ManagedZones.List(d.projectID)
	zones, err := zonesCall.Do()
	if err != nil {
		return nil, nil, err
	}
	for _, zone := range zones.ManagedZones {
		rrlc := d.dnsClient.ResourceRecordSets.List(d.projectID, zone.Name)
		rrl, err := rrlc.Do()
		if err != nil {
			return nil, nil, err
		}
		for _, rr := range rrl.Rrsets {
			if rr.Name == name {
				return zone, rr, nil
			}
		}
	}

	return nil, nil, fmt.Errorf("Host not found")
}
