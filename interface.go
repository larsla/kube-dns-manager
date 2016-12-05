package main

type Interface interface {
	Update(name string, ip []string, recordType string, ttl int64) error
	Delete(name, recordType string) error
}
