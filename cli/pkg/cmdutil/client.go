package cmdutil

import (
	"log"
	"time"

	"google.golang.org/grpc"
)

func GetGrpcConn() (*grpc.ClientConn, error) {
	address := GetVal("endpoint")
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	if err != nil {
		log.Fatalf("can not connect: %v", err)
	}

	return conn, err
}
