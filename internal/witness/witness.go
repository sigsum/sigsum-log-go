package witness

import (
	"context"
	"sync"

	"sigsum.org/sigsum-go/pkg/api"
	"sigsum.org/sigsum-go/pkg/checkpoint"
	"sigsum.org/sigsum-go/pkg/client"
	"sigsum.org/sigsum-go/pkg/crypto"
	"sigsum.org/sigsum-go/pkg/log"
	"sigsum.org/sigsum-go/pkg/policy"
	"sigsum.org/sigsum-go/pkg/requests"
	"sigsum.org/sigsum-go/pkg/types"
)

type GetConsistencyProofFunc func(ctx context.Context, req *requests.ConsistencyProof) (types.ConsistencyProof, error)

// Not concurrency safe, due to updates of prevSize.
type witness struct {
	client    api.Witness
	publicKey crypto.PublicKey
	keyHash   crypto.Hash
	prevSize  uint64
}

func newWitness(w *policy.Entity) *witness {
	return &witness{
		client:    client.New(client.Config{URL: w.URL, UserAgent: "Sigsum log-go server"}),
		publicKey: w.PublicKey,
		keyHash:   crypto.HashBytes(w.PublicKey[:]),
		prevSize:  0,
	}
}

// Pack key hash and cosignature together, so they can be sent over a channel.
type cosignatureItem struct {
	keyHash crypto.Hash
	cs      types.Cosignature
}

func (w *witness) getCosignature(ctx context.Context, cp *checkpoint.Checkpoint, getConsistencyProof GetConsistencyProofFunc) (cosignatureItem, error) {
	// TODO: Limit number of attempts.
	for {
		proof, err := getConsistencyProof(ctx, &requests.ConsistencyProof{
			OldSize: w.prevSize,
			NewSize: cp.TreeHead.Size,
		})
		if err != nil {
			return cosignatureItem{}, err
		}
		signatures, err := w.client.AddCheckpoint(ctx, requests.AddCheckpoint{
			OldSize:    w.prevSize,
			Proof:      proof,
			Checkpoint: *cp,
		})
		if err == nil {
			cs, err := cp.VerifyCosignatureByKey(signatures, &w.publicKey)
			if err != nil {
				return cosignatureItem{}, err
			}
			w.prevSize = cp.Size
			return cosignatureItem{keyHash: w.keyHash, cs: cs}, nil
		}
		if oldSize, ok := api.ErrorConflictOldSize(err); ok {
			w.prevSize = oldSize
		} else {
			return cosignatureItem{}, err
		}
	}
}

type CosignatureCollector struct {
	origin              string
	keyId               checkpoint.KeyId
	getConsistencyProof GetConsistencyProofFunc
	witnesses           []*witness
}

func NewCosignatureCollector(logPublicKey *crypto.PublicKey, witnesses []policy.Entity,
	getConsistencyProof GetConsistencyProofFunc) *CosignatureCollector {
	origin := types.SigsumCheckpointOrigin(logPublicKey)

	collector := CosignatureCollector{
		origin:              origin,
		keyId:               checkpoint.NewLogKeyId(origin, logPublicKey),
		getConsistencyProof: getConsistencyProof,
	}
	for _, w := range witnesses {
		collector.witnesses = append(collector.witnesses,
			newWitness(&w))
	}
	return &collector
}

// Queries all witnesses in parallel, blocks until we have result or error from each of them.
// Must not be concurrently called.
func (c *CosignatureCollector) GetCosignatures(ctx context.Context, sth *types.SignedTreeHead) map[crypto.Hash]types.Cosignature {
	cp := checkpoint.Checkpoint{
		SignedTreeHead: *sth,
		Origin:         c.origin,
		KeyId:          c.keyId,
	}

	wg := sync.WaitGroup{}

	ch := make(chan cosignatureItem)

	// Query witnesses in parallel
	for i, w := range c.witnesses {
		i, w := i, w // New variables for each round through the loop.
		wg.Add(1)
		go func() {
			cs, err := w.getCosignature(ctx, &cp, c.getConsistencyProof)
			if err != nil {
				log.Error("querying witness %d failed: %v", i, err)
				// TODO: Temporarily stop querying this witness?
			} else {
				ch <- cs
			}
			wg.Done()
		}()
	}
	go func() { wg.Wait(); close(ch) }()

	cosignatures := make(map[crypto.Hash]types.Cosignature)
	for i := range ch {
		// TODO: Check that cosignature timestamp is reasonable?
		cosignatures[i.keyHash] = i.cs
	}
	return cosignatures
}
