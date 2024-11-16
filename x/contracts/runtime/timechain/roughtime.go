package timechain

import (
    "crypto/ed25519"
    "encoding/binary"
    "errors"
    "fmt"
    "sort"
    "sync"
    "time"
    
    "github.com/cloudflare/roughtime"
    "golang.org/x/crypto/sha3"
)

type RoughtimeClient struct {
    servers     []RoughtimeServer
    minServers  int
    mutex       sync.RWMutex
}

type RoughtimeServer struct {
    address  string
    pubKey   ed25519.PublicKey
}

type RoughtimeProof struct {
    ServerName  string
    Timestamp   uint64
    Signature   []byte
    RadiusMicros uint32
    PublicKey   ed25519.PublicKey
}

func NewRoughtimeClient(minServers int) *RoughtimeClient {
    return &RoughtimeClient{
        servers: []RoughtimeServer{
            {
                address:  "roughtime.cloudflare.com:2002",
                pubKey:   roughtime.CloudflarePublicKey,
            },
            // Add more servers here
        },
        minServers: minServers,
    }
}

func (rc *RoughtimeClient) GetVerifiedTimeWithProof() (time.Time, *RoughtimeProof, error) {
    rc.mutex.Lock()
    defer rc.mutex.Unlock()

    var proofs []*RoughtimeProof
    var timestamps []time.Time

    // Query multiple servers
    for _, server := range rc.servers {
        result, err := roughtime.QueryServer(server.address, server.pubKey)
        if err != nil {
            continue
        }

        proof := &RoughtimeProof{
            ServerName:    server.address,
            Timestamp:     uint64(result.Midpoint.UnixNano()),
            Signature:     result.Signature,
            RadiusMicros: uint32(result.Radius.Microseconds()),
            PublicKey:    server.pubKey,
        }
        
        proofs = append(proofs, proof)
        timestamps = append(timestamps, result.Midpoint)
    }

    if len(proofs) < rc.minServers {
        return time.Time{}, nil, errors.New("insufficient time proofs")
    }

    // Calculate median timestamp
    medianTime := calculateMedianTime(timestamps)
    medianProof := selectMedianProof(proofs, medianTime)

    return medianTime, medianProof, nil
}

// calculateMedianTime returns the median time from a slice of timestamps
func calculateMedianTime(timestamps []time.Time) time.Time {
    if len(timestamps) == 0 {
        return time.Time{}
    }

    // Convert to Unix nanos for easier sorting
    nanos := make([]int64, len(timestamps))
    for i, t := range timestamps {
        nanos[i] = t.UnixNano()
    }

    // Sort the nanos
    sort.Slice(nanos, func(i, j int) bool {
        return nanos[i] < nanos[j]
    })

    // Get median
    median := nanos[len(nanos)/2]
    return time.Unix(0, median)
}

// selectMedianProof selects the proof closest to the median time
func selectMedianProof(proofs []*RoughtimeProof, medianTime time.Time) *RoughtimeProof {
    if len(proofs) == 0 {
        return nil
    }

    medianNano := medianTime.UnixNano()
    var closestProof *RoughtimeProof
    var minDiff int64 = 1<<63 - 1 // Max int64

    for _, proof := range proofs {
        diff := abs(int64(proof.Timestamp) - medianNano)
        if diff < minDiff {
            minDiff = diff
            closestProof = proof
        }
    }

    return closestProof
}

// abs returns the absolute value of an int64
func abs(n int64) int64 {
    if n < 0 {
        return -n
    }
    return n
}

// VerifyTimeProof verifies a Roughtime proof
func (rc *RoughtimeClient) VerifyTimeProof(proof *RoughtimeProof) error {
    if proof == nil {
        return errors.New("nil proof")
    }

    // Create hash of the time data
    hasher := sha3.New256()
    timeBytes := make([]byte, 8)
    binary.BigEndian.PutUint64(timeBytes, proof.Timestamp)
    hasher.Write(timeBytes)
    
    radiusBytes := make([]byte, 4)
    binary.BigEndian.PutUint32(radiusBytes, proof.RadiusMicros)
    hasher.Write(radiusBytes)
    
    hash := hasher.Sum(nil)

    // Verify signature
    if !ed25519.Verify(proof.PublicKey, hash, proof.Signature) {
        return errors.New("invalid signature")
    }

    return nil
}

// AddServer adds a new Roughtime server to the client
func (rc *RoughtimeClient) AddServer(address string, pubKey ed25519.PublicKey) {
    rc.mutex.Lock()
    defer rc.mutex.Unlock()

    rc.servers = append(rc.servers, RoughtimeServer{
        address: address,
        pubKey:  pubKey,
    })
}

// GetServers returns the list of configured servers
func (rc *RoughtimeClient) GetServers() []RoughtimeServer {
    rc.mutex.RLock()
    defer rc.mutex.RUnlock()

    servers := make([]RoughtimeServer, len(rc.servers))
    copy(servers, rc.servers)
    return servers
}

// SetMinServers sets the minimum number of required server responses
func (rc *RoughtimeClient) SetMinServers(min int) {
    rc.mutex.Lock()
    defer rc.mutex.Unlock()

    rc.minServers = min
}

// GetTimeWithRetry attempts to get verified time with retries
func (rc *RoughtimeClient) GetTimeWithRetry(maxAttempts int, retryDelay time.Duration) (time.Time, *RoughtimeProof, error) {
    var lastErr error

    for attempt := 0; attempt < maxAttempts; attempt++ {
        verifiedTime, proof, err := rc.GetVerifiedTimeWithProof()
        if err == nil {
            return verifiedTime, proof, nil
        }
        lastErr = err

        if attempt < maxAttempts-1 {
            time.Sleep(retryDelay)
        }
    }

    return time.Time{}, nil, fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}

// ValidateTimestamp checks if a timestamp is within acceptable bounds
func (rc *RoughtimeClient) ValidateTimestamp(timestamp time.Time, tolerance time.Duration) error {
    verifiedTime, _, err := rc.GetVerifiedTimeWithProof()
    if err != nil {
        return fmt.Errorf("failed to get verified time: %w", err)
    }

    diff := verifiedTime.Sub(timestamp)
    if abs(int64(diff)) > int64(tolerance) {
        return fmt.Errorf("timestamp outside tolerance: diff=%v", diff)
    }

    return nil
}

// GetProofHash generates a unique hash for a proof
func (proof *RoughtimeProof) GetProofHash() []byte {
    hasher := sha3.New256()
    
    // Hash server name
    hasher.Write([]byte(proof.ServerName))
    
    // Hash timestamp
    timeBytes := make([]byte, 8)
    binary.BigEndian.PutUint64(timeBytes, proof.Timestamp)
    hasher.Write(timeBytes)
    
    // Hash radius
    radiusBytes := make([]byte, 4)
    binary.BigEndian.PutUint32(radiusBytes, proof.RadiusMicros)
    hasher.Write(radiusBytes)
    
    // Hash signature
    hasher.Write(proof.Signature)
    
    // Hash public key
    hasher.Write(proof.PublicKey)
    
    return hasher.Sum(nil)
}