package main

import (
	"flag"
	pb "gpu-rpc/proto"
	"log"
	"net"
	"os"
	"time"

	nvml "github.com/mindprince/gonvml"

	grpc "google.golang.org/grpc"
)

const defaultSocket = "/var/lib/kubelet/gpu.sock"

type GPUInfoServer struct {
	socket string
	server *grpc.Server
}

func NewGPUInfoServer(s string) *GPUInfoServer {
	return &GPUInfoServer{
		socket: s,
	}
}

func getGPUMemoryCapacityAndUsed() ([]int64, []int64, error) {
	cap := make([]int64, 0)
	used := make([]int64, 0)
	if err := nvml.Initialize(); err != nil {
		log.Printf("Can't initialize NVML: %v \n", err)
		return cap, used, err
	}
	n, err := nvml.DeviceCount()
	if err != nil {
		log.Printf("Failed to fetch Can't initialize NVML: %v \n", err)
		return cap, used, err
	}
	cap = make([]int64, n)
	used = make([]int64, n)
	for i := uint(0); i < n; i++ {
		d, err := nvml.DeviceHandleByIndex(i)
		if err != nil {
			log.Printf("Failed to get device(%d): %v", i, err)
			return cap, used, err
		}
		t, u, err := d.MemoryInfo()
		if err != nil {
			log.Printf("Failed to get device(%d) GPU info: %v", i, err)
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
			log.Printf("Failed to get node GPU memory:%v\n", err)
			continue
		}
		err = stream.Send(&pb.GPUMemoryResponse{Cap: cap, Used: used})
		if err != nil {
			log.Printf("Failed to send GPU memory response: %v\n", err)
			break
		}
		log.Printf("Cap:%v, Used: %v\n", cap, used)
	}
	return nil
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
		log.Printf("Could not start GPUInfoServer: %s\n", err)
		s.Stop()
		return err
	}
	log.Println("Starting to serve on", s.socket)
	return nil
}

func main() {
	var socketPath string
	flag.StringVar(&socketPath, "socket", defaultSocket, "GPU grpc server socket path")
	flag.Parse()
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
			server = NewGPUInfoServer(socketPath)
			if err := server.Serve(); err != nil {
				time.Sleep(2 * time.Second)
			} else {
				restart = false
			}
		}
		time.Sleep(30 * time.Second)
	}
}
