package main

import (
	"context"
	"fmt"
	pb "gpu-rpc/proto"
	"log"
	"net"
	"time"

	grpc "google.golang.org/grpc"
)

const socket = "/var/lib/kubelet/gpu.sock"

// dial establishes the gRPC communication with the registered device plugin.
func dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	c, err := grpc.Dial(unixSocketPath, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithTimeout(timeout),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func main() {
	conn, err := dial(socket, 5*time.Second)
	if err != nil {
		log.Fatalf("dial error: %v\n", err)
	}
	defer conn.Close()
	req := new(pb.Request)
	client := pb.NewGPUInfoServiceClient(conn)
	stream, _ := client.GetGPUMemoryCapacityAndUsed(context.Background(), req)
	for {
		response, _ := stream.Recv()
		time.Sleep(2 * time.Second)
		// 实例化 UserInfoService 微服务的客户端
		// 调用服务
		fmt.Printf("Recevied: %v\n", response)
	}
}
