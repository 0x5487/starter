package event

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/jasonsoft/log/v2"
	"github.com/jasonsoft/starter/internal/pkg/config"
	eventProto "github.com/jasonsoft/starter/pkg/event/proto"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// RunCmd 是 event service 的進入口
var RunCmd = &cobra.Command{
	Use:   "event",
	Short: "",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		defer log.Flush()
		defer func() {
			if r := recover(); r != nil {
				// unknown error
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("unknown error: %v", r)
				}
				trace := make([]byte, 4096)
				runtime.Stack(trace, true)
				log.Str("stack_trace", string(trace)).Err(err).Panic("unknown error")
			}
		}()

		// fix gorm NowFunc
		// gorm.NowFunc = func() time.Time {
		// 	return time.Now().UTC()
		// }

		config.EnvPrefix = "STARTER"
		cfg := config.New("app.yml")
		err := initialize(cfg)
		if err != nil {
			log.Fatalf("main: initialize failed: %v", err)
			return
		}

		// start grpc servers
		lis, err := net.Listen("tcp", cfg.Event.GRPCBind)
		if err != nil {
			log.Fatalf("main: failed to grpc listen: %v", err)
		}

		grpcServer := grpc.NewServer(
			grpc.KeepaliveParams(
				keepalive.ServerParameters{
					Time:    (time.Duration(5) * time.Second), // Ping the client if it is idle for 5 seconds to ensure the connection is still active
					Timeout: (time.Duration(5) * time.Second), // Wait 5 second for the ping ack before assuming the connection is dead
				},
			),
			grpc.KeepaliveEnforcementPolicy(
				keepalive.EnforcementPolicy{
					MinTime:             (time.Duration(2) * time.Second), // If a client pings more than once every 2 seconds, terminate the connection
					PermitWithoutStream: true,                             // Allow pings even when there are no active streams
				},
			),
			grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
				grpcInterceptor(),
			)),
		)
		eventProto.RegisterEventServiceServer(grpcServer, _eventServer)
		log.Infof("event grpc service listen on %s", cfg.Event.GRPCBind)
		go func() {
			if err = grpcServer.Serve(lis); err != nil {
				log.Fatalf("main: failed to start agent order grpc server: %v", err)
			}
		}()

		stopChan := make(chan os.Signal, 1)
		signal.Notify(stopChan, syscall.SIGINT, syscall.SIGKILL, syscall.SIGHUP, syscall.SIGTERM)
		<-stopChan
		log.Info("main: shutting down server...")

		grpcServer.GracefulStop()
		log.Info("main: grpc server gracefully stopped")

	},
}
