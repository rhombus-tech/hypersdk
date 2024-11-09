package events

const (
    DefaultEnabled       = false
    DefaultMaxBlockSize = uint64(1000)
    DefaultPingTimeout  = uint64(100)
)

type Config struct {
    Enabled       bool       `json:"enabled,omitempty" yaml:"enabled,omitempty"`
    MaxBlockSize  uint64     `json:"maxBlockSize,omitempty" yaml:"max_block_size,omitempty"`
    PingTimeout   uint64     `json:"pingTimeout,omitempty" yaml:"ping_timeout,omitempty"`
    PongValidators [][32]byte `json:"pongValidators,omitempty" yaml:"pong_validators,omitempty"`
}

func NewConfig() *Config {
    return &Config{
        Enabled:      DefaultEnabled,
        MaxBlockSize: DefaultMaxBlockSize,
        PingTimeout:  DefaultPingTimeout,
        PongValidators: make([][32]byte, 0),
    }
}