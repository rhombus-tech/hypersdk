package timechain

import (
    "bytes"
    "encoding/hex"
    "errors"
    "fmt"
    "sync"

    "golang.org/x/crypto/sha3"
    "github.com/ava-labs/avalanchego/utils/hashing"
    "github.com/ava-labs/avalanchego/utils/wrappers"
)

// Keep your existing TimeMerkleTree struct
type TimeMerkleTree struct {
    leaves [][]byte
    nodes  map[string][]byte
    mutex  sync.RWMutex
}

// Keep your existing NewTimeMerkleTree
func NewTimeMerkleTree() *TimeMerkleTree {
    return &TimeMerkleTree{
        leaves: make([][]byte, 0),
        nodes:  make(map[string][]byte),
    }
}

// Keep your existing updateMerkleRoot
func (tmt *TimeMerkleTree) updateMerkleRoot() {
    tmt.mutex.Lock()
    defer tmt.mutex.Unlock()

    if len(tmt.leaves) == 0 {
        return
    }

    currentLevel := tmt.leaves
    for len(currentLevel) > 1 {
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
            tmt.nodes[string(parent)] = parent
        }
        
        currentLevel = nextLevel
    }
}

// Add these new methods:

// AddLeaf adds a new leaf to the tree and updates the root
func (tmt *TimeMerkleTree) AddLeaf(data []byte) {
    tmt.mutex.Lock()
    defer tmt.mutex.Unlock()

    tmt.leaves = append(tmt.leaves, data)
    tmt.updateMerkleRoot()
}

// GetRoot returns the current root hash
func (tmt *TimeMerkleTree) GetRoot() []byte {
    tmt.mutex.RLock()
    defer tmt.mutex.RUnlock()

    if len(tmt.leaves) == 0 {
        return nil
    }

    currentLevel := tmt.leaves
    for len(currentLevel) > 1 {
        currentLevel = tmt.getParentLevel(currentLevel)
    }
    return currentLevel[0]
}

// GenerateProof generates a Merkle proof for a leaf at the given index
func (tmt *TimeMerkleTree) GenerateProof(index int) ([][]byte, error) {
    tmt.mutex.RLock()
    defer tmt.mutex.RUnlock()

    if index >= len(tmt.leaves) {
        return nil, errors.New("index out of range")
    }

    proof := make([][]byte, 0)
    currentIndex := index
    currentLevel := tmt.leaves

    for len(currentLevel) > 1 {
        siblingIndex := currentIndex
        if currentIndex%2 == 0 {
            siblingIndex++
        } else {
            siblingIndex--
        }

        if siblingIndex < len(currentLevel) {
            proof = append(proof, currentLevel[siblingIndex])
        }

        currentLevel = tmt.getParentLevel(currentLevel)
        currentIndex /= 2
    }

    return proof, nil
}

// getParentLevel calculates the parent level nodes
func (tmt *TimeMerkleTree) getParentLevel(level [][]byte) [][]byte {
    nextLevel := make([][]byte, 0, (len(level)+1)/2)
    
    for i := 0; i < len(level); i += 2 {
        var right []byte
        left := level[i]
        
        if i+1 < len(level) {
            right = level[i+1]
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

// VerifyProof verifies a Merkle proof
func (tmt *TimeMerkleTree) VerifyProof(leaf []byte, proof [][]byte, root []byte) bool {
    currentHash := leaf
    
    for _, sibling := range proof {
        hash := sha3.New256()
        
        // Determine order based on current hash
        if bytes.Compare(currentHash, sibling) < 0 {
            hash.Write(currentHash)
            hash.Write(sibling)
        } else {
            hash.Write(sibling)
            hash.Write(currentHash)
        }
        
        currentHash = hash.Sum(nil)
    }
    
    return bytes.Equal(currentHash, root)
}

// GetLeafCount returns the number of leaves in the tree
func (tmt *TimeMerkleTree) GetLeafCount() int {
    tmt.mutex.RLock()
    defer tmt.mutex.RUnlock()
    return len(tmt.leaves)
}

// Reset clears the tree
func (tmt *TimeMerkleTree) Reset() {
    tmt.mutex.Lock()
    defer tmt.mutex.Unlock()
    
    tmt.leaves = make([][]byte, 0)
    tmt.nodes = make(map[string][]byte)
}