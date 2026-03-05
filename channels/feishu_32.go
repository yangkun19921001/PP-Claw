//go:build !amd64 && !arm64 && !riscv64 && !mips64 && !ppc64

package channels

import (
	"errors"

	"github.com/yangkun19921001/PP-Claw/bus"
	"go.uber.org/zap"
)

func init() {
	RegisterFactory("feishu", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return nil, errors.New("feishu channel is not supported on 32-bit architectures")
	})
}
