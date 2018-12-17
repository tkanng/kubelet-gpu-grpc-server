package main

import (
	"fmt"
	pb "gpu-rpc/proto"
	"log"
	"net"
	"os"
	"time"

	nvml "github.com/mindprince/gonvml"

	grpc "google.golang.org/grpc"
)

const serverSocket = "/var/lib/kubelet/gpu.sock"

// 定义服务端实现约定的接口
type GPUInfoServer struct {
	socket string
	server *grpc.Server
}

func NewGPUInfoServer() *GPUInfoServer {
	return &GPUInfoServer{
		socket: serverSocket,
	}
}

// 实现 interface
func getGPUMemoryCapacityAndUsed() ([]int64, []int64, error) {
	// 模拟在数据库中查找用户信息
	cap := make([]int64, 0)
	used := make([]int64, 0)
	if err := nvml.Initialize(); err != nil {
		log.Println(err)
		return cap, used, err
	}
	n, err := nvml.DeviceCount()
	if err != nil {
		log.Println(err)
		return cap, used, err
	}
	cap = make([]int64, n)
	used = make([]int64, n)
	for i := uint(0); i < n; i++ {
		d, err := nvml.DeviceHandleByIndex(i)
		if err != nil {
			log.Println(err)
			return cap, used, err
		}
		t, u, err := d.MemoryInfo()
		if err != nil {
			log.Println(err)
			return cap, used, err
		}
		cap[i] = int64(t) / (1024 * 1024)
		used[i] = int64(u) / (1024 * 1024)
	}
	return cap, used, nil
}

func (s *GPUInfoServer) GetGPUMemoryCapacityAndUsed(rect *pb.Request, stream pb.GPUInfoService_GetGPUMemoryCapacityAndUsedServer) error {
	for {
		time.Sleep(4 * time.Second)
		cap, used, err := getGPUMemoryCapacityAndUsed()
		if err != nil {
			fmt.Printf("Error:%v", err)
			continue
		}
		stream.Send(&pb.GPUMemoryResponse{Cap: cap, Used: used})
		fmt.Printf("cap:%v, used: %v", cap, used)
	}
}

func (s *GPUInfoServer) cleanup() error {
	if err := os.Remove(s.socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
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

func (s *GPUInfoServer) Start() error {
	err := s.cleanup()
	if err != nil {
		return err
	}
	sock, err := net.Listen("unix", s.socket)
	if err != nil {
		return err
	}
	s.server = grpc.NewServer([]grpc.ServerOption{}...)
	pb.RegisterGPUInfoServiceServer(s.server, s)
	go s.server.Serve(sock)

	// Wait for server to start by launching a blocking connexion
	conn, err := dial(s.socket, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Stop stops the gRPC server
func (s *GPUInfoServer) Stop() error {
	if s.server == nil {
		return nil
	}
	s.server.Stop()
	s.server = nil
	return s.cleanup()
}

func (s *GPUInfoServer) Serve() error {
	err := s.Start()
	if err != nil {
		log.Printf("Could not start GPUInfoServer: %s", err)
		s.Stop()
		return err
	}
	log.Println("Starting to serve on", s.socket)
	return nil
}

func main() {
	log.Println("Loading NVML")
	if err := nvml.Initialize(); err != nil {
		log.Printf("Failed to initialize NVML: %s.", err)
		log.Printf("If this is a GPU node, did you set the docker default runtime to `nvidia`?")
		log.Printf("You can check the prerequisites at: https://github.com/NVIDIA/k8s-device-plugin#prerequisites")
		log.Printf("You can learn how to set the runtime at: https://github.com/NVIDIA/k8s-device-plugin#quick-start")

		log.Println("Waiting indefinitely.")
		select {}
	}
	defer func() { log.Println("Shutdown of NVML returned:", nvml.Shutdown()) }()

	log.Println("Fetching devices.")
	n, err := nvml.DeviceCount()
	if err != nil || n == 0 {
		log.Println("No devices found. Waiting indefinitely.")
		select {}
	}
	restart := true
	var server *GPUInfoServer
	for {
		if restart {
			if server != nil {
				server.Stop()
			}
			server = NewGPUInfoServer()
			if err := server.Serve(); err != nil {
				time.Sleep(2 * time.Second)
			} else {
				restart = false
			}
		}
	}
}