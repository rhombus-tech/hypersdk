package timechain

import (
    "bytes"
    "encoding/binary"
    "errors"
    "fmt"
    "time"

    "golang.org/x/crypto/sha3"
    "github.com/ava-labs/avalanchego/utils/crypto"
    "github.com/ava-labs/avalanchego/utils/formatting"
    "github.com/ava-labs/hypersdk/codec"
)

// Common error definitions that could be in a separate errors.go file
var (
    ErrInvalidProof        = errors.New("invalid merkle proof")
    ErrInvalidTimeSequence = errors.New("invalid time sequence")
    ErrInvalidHashChain    = errors.New("invalid hash chain")
    ErrInsufficientProofs  = errors.New("insufficient roughtime proofs")
    ErrTimeViolation       = errors.New("time sequence violation")
    ErrOutsideTimeWindow   = errors.New("entry outside time window")
    ErrInvalidMerkleRoot   = errors.New("invalid merkle root")
)

// Common interface definitions that could be in a separate types.go file
type Transaction interface {
    ID() ids.ID
    Bytes() []byte
    Verify() error
}

type TimeProver interface {
    GetVerifiedTimeWithProof() (time.Time, *RoughtimeProof, error)
    VerifyTimeProof(*RoughtimeProof) error
}

type ProofVerifier interface {
    VerifyProof(*MerkleProof, []byte) error
    VerifySequence([]*SequenceEntry) error
}

type HashGenerator interface {
    GenerateHash([]byte) []byte
    CombineHashes(left, right []byte) []byte
}

type MerkleProof struct {
    Index     uint64
    Leaf      []byte
    Path      [][]byte
    Root      []byte
}

func (tmt *TimeMerkleTree) generateMerkleProof(index int) *MerkleProof {
    tmt.mutex.RLock()
    defer tmt.mutex.RUnlock()

    if index >= len(tmt.leaves) {
        return nil
    }

    proof := &MerkleProof{
        Index: uint64(index),
        Leaf:  tmt.leaves[index],
        Path:  make([][]byte, 0),
    }

    currentLevel := tmt.leaves
    currentIndex := index

    for len(currentLevel) > 1 {
        var sibling []byte
        if currentIndex%2 == 0 {
            if currentIndex+1 < len(currentLevel) {
                sibling = currentLevel[currentIndex+1]
            } else {
                sibling = currentLevel[currentIndex]
            }
        } else {
            sibling = currentLevel[currentIndex-1]
        }

        proof.Path = append(proof.Path, sibling)
        currentIndex /= 2
        currentLevel = tmt.getNextLevel(currentLevel)
    }

    proof.Root = currentLevel[0]
    return proof
}

func (tmt *TimeMerkleTree) getNextLevel(currentLevel [][]byte) [][]byte {
    nextLevel := make([][]byte, 0)
    for i := 0; i < len(currentLevel); i += 2 {
        var right []byte
        left := currentLevel[i]
        
        if i+1 < len(currentLevel) {
            right = currentLevel[i+1]
        } else {
            right = left
        }
        
        hash := sha3.New256()
        hash.Write(left)
        hash.Write(right)
        parent := hash.Sum(nil)
        
        nextLevel = append(nextLevel, parent)
    }
    return nextLevel
}