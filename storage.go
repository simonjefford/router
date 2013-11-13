package main

type Iterator interface {
	Next(result interface{}) bool
	Err() error
}

type Storage interface {
	Applications() (Iterator, error)
	Routes() (Iterator, error)
	Open() error
	Close()
}
