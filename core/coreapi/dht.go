package coreapi

import (
	"context"
	"errors"
	"fmt"

	coreiface "github.com/ipfs/go-ipfs/core/coreapi/interface"
	caopts "github.com/ipfs/go-ipfs/core/coreapi/interface/options"

	blockservice "gx/ipfs/QmNozJswSuwiZspexEHcQo5GMqpzM5exUGjNW6s4AAipUX/go-blockservice"
	cid "gx/ipfs/QmPSQnBKM9g7BaUcZCvswUJVscQ1ipjmwxN5PXCjkp9EQ7/go-cid"
	offline "gx/ipfs/QmPXcrGQQEEPswwg6YiE2WLk8qkmvncZ7zphMKKP8bXqY3/go-ipfs-exchange-offline"
	cidutil "gx/ipfs/QmQJSeE3CX4zos9qeaG8EhecEK9zvrTEfTG84J8C5NVRwt/go-cidutil"
	routing "gx/ipfs/QmQRfifvvbJ8xTKj4KX1VvGWK26hnPiy8eQvW1hmjc82nD/go-libp2p-routing"
	pstore "gx/ipfs/QmSJ36wcYQyEViJUWUEhJU81tw1KdakTKqLLHbvYbA9zDv/go-libp2p-peerstore"
	dag "gx/ipfs/QmTGpm48qm4fUZ9E5hMXy4ZngJUYCMKu15rTMVR3BSEnPm/go-merkledag"
	peer "gx/ipfs/QmbNepETomvmXfz1X5pHNFD2QuPqnqi47dTd94QJWSorQ3/go-libp2p-peer"
	blockstore "gx/ipfs/QmfUhZX9KpvJiuiziUzP2cjhRAyqHJURsPgRKn1cdDZMKa/go-ipfs-blockstore"
)

type DhtAPI CoreAPI

func (api *DhtAPI) FindPeer(ctx context.Context, p peer.ID) (pstore.PeerInfo, error) {
	pi, err := api.node.Routing.FindPeer(ctx, peer.ID(p))
	if err != nil {
		return pstore.PeerInfo{}, err
	}

	return pi, nil
}

func (api *DhtAPI) FindProviders(ctx context.Context, p coreiface.Path, opts ...caopts.DhtFindProvidersOption) (<-chan pstore.PeerInfo, error) {
	settings, err := caopts.DhtFindProvidersOptions(opts...)
	if err != nil {
		return nil, err
	}

	rp, err := api.core().ResolvePath(ctx, p)
	if err != nil {
		return nil, err
	}

	numProviders := settings.NumProviders
	if numProviders < 1 {
		return nil, fmt.Errorf("number of providers must be greater than 0")
	}

	pchan := api.node.Routing.FindProvidersAsync(ctx, rp.Cid(), numProviders)
	return pchan, nil
}

func (api *DhtAPI) Provide(ctx context.Context, path coreiface.Path, opts ...caopts.DhtProvideOption) error {
	settings, err := caopts.DhtProvideOptions(opts...)
	if err != nil {
		return err
	}

	if api.node.Routing == nil {
		return errors.New("cannot provide in offline mode")
	}

	rp, err := api.core().ResolvePath(ctx, path)
	if err != nil {
		return err
	}

	c := rp.Cid()

	has, err := api.node.Blockstore.Has(c)
	if err != nil {
		return err
	}

	if !has {
		return fmt.Errorf("block %s not found locally, cannot provide", c)
	}

	if settings.Recursive {
		err = provideKeysRec(ctx, api.node.Routing, api.node.Blockstore, []cid.Cid{c})
	} else {
		err = provideKeys(ctx, api.node.Routing, []cid.Cid{c})
	}
	if err != nil {
		return err
	}

	return nil
}

func provideKeys(ctx context.Context, r routing.IpfsRouting, cids []cid.Cid) error {
	for _, c := range cids {
		err := r.Provide(ctx, c, true)
		if err != nil {
			return err
		}
	}
	return nil
}

func provideKeysRec(ctx context.Context, r routing.IpfsRouting, bs blockstore.Blockstore, cids []cid.Cid) error {
	provided := cidutil.NewStreamingSet()

	errCh := make(chan error)
	go func() {
		dserv := dag.NewDAGService(blockservice.New(bs, offline.Exchange(bs)))
		for _, c := range cids {
			err := dag.EnumerateChildrenAsync(ctx, dag.GetLinksDirect(dserv), c, provided.Visitor(ctx))
			if err != nil {
				errCh <- err
			}
		}
	}()

	for {
		select {
		case k := <-provided.New:
			err := r.Provide(ctx, k, true)
			if err != nil {
				return err
			}
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (api *DhtAPI) core() coreiface.CoreAPI {
	return (*CoreAPI)(api)
}
