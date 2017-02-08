package filestore

import (
	"fmt"

	bs "github.com/ipfs/go-ipfs/blocks/blockstore"
	u "github.com/ipfs/go-ipfs/blocks/blockstore/util"
	"github.com/ipfs/go-ipfs/pin"
	ds "gx/ipfs/QmRWDav6mzWseLWeYfVd5fvUKiVe9xNH29YfMF438fG364/go-datastore"
	cid "gx/ipfs/QmV5gPoRsjN1Gid3LMdNZTyfCtP2DsvqEbMAmz82RmmiGk/go-cid"
)

// Note: Like util/remove.go but allows removal of pinned block from
//   one store if it is also in the other

type Deleter interface {
	DeleteBlock(c *cid.Cid) error
}

func RmBlocks(fs *Filestore, lock bs.GCLocker, pins pin.Pinner, cids []*cid.Cid, opts u.RmBlocksOpts) (<-chan interface{}, error) {
	// make the channel large enough to hold any result to avoid
	// blocking while holding the GCLock
	out := make(chan interface{}, len(cids))

	var blocks Deleter
	switch opts.Prefix {
	case FilestorePrefix.String():
		blocks = fs.fm
	case bs.BlockPrefix.String():
		blocks = fs.bs
	default:
		return nil, fmt.Errorf("Unknown prefix: %s\n", opts.Prefix)
	}

	go func() {
		defer close(out)

		unlocker := lock.GCLock()
		defer unlocker.Unlock()

		stillOkay := FilterPinned(fs, pins, out, cids, blocks)

		for _, c := range stillOkay {
			err := blocks.DeleteBlock(c)
			if err != nil && opts.Force && (err == bs.ErrNotFound || err == ds.ErrNotFound) {
				// ignore non-existent blocks
			} else if err != nil {
				out <- &u.RemovedBlock{Hash: c.String(), Error: err.Error()}
			} else if !opts.Quiet {
				out <- &u.RemovedBlock{Hash: c.String()}
			}
		}
	}()
	return out, nil
}

func FilterPinned(fs *Filestore, pins pin.Pinner, out chan<- interface{}, cids []*cid.Cid, foundIn Deleter) []*cid.Cid {
	stillOkay := make([]*cid.Cid, 0, len(cids))
	res, err := pins.CheckIfPinned(cids...)
	if err != nil {
		out <- &u.RemovedBlock{Error: fmt.Sprintf("pin check failed: %s", err)}
		return nil
	}
	for _, r := range res {
		if !r.Pinned() || AvailableElsewhere(fs, foundIn, r.Key) {
			stillOkay = append(stillOkay, r.Key)
		} else {
			out <- &u.RemovedBlock{
				Hash:  r.Key.String(),
				Error: r.String(),
			}
		}
	}
	return stillOkay
}

func AvailableElsewhere(fs *Filestore, foundIn Deleter, c *cid.Cid) bool {
	switch {
	case fs.fm == foundIn:
		have, _ := fs.bs.Has(c)
		return have
	case fs.bs == foundIn:
		have, _ := fs.fm.Has(c)
		return have
	default:
		// programmer error
		panic("invalid pointer for foundIn")
	}
}
