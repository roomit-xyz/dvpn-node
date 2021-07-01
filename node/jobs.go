package node

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	hubtypes "github.com/sentinel-official/hub/types"

	"github.com/sentinel-official/dvpn-node/types"
)

func (n *Node) jobSetSessions() error {
	n.Log().Info("Starting a job", "name", "set_sessions", "interval", n.IntervalSetSessions())

	t := time.NewTicker(n.IntervalSetSessions())
	for ; ; <-t.C {
		peers, err := n.Service().Peers()
		if err != nil {
			return err
		}

		count := len(peers)
		if count > 0 {
			n.Log().Info("Validating the usage of connected peers", "count", count)
		}

		for i := 0; i < count; i++ {
			var item types.Session
			n.Database().Model(
				&types.Session{},
			).Where(
				&types.Session{
					Key: peers[i].Key,
				},
			).First(&item)

			if item.ID == 0 {
				n.Log().Info("Unknown connected peer", "key", peers[i].Key)
				if err := n.RemovePeer(peers[i].Key); err != nil {
					return err
				}

				continue
			}

			n.Database().Model(
				&types.Session{},
			).Where(
				&types.Session{
					ID: item.ID,
				},
			).Updates(
				&types.Session{
					Upload:   peers[i].Upload,
					Download: peers[i].Download,
				},
			)

			var (
				available = sdk.NewInt(item.Available)
				consumed  = sdk.NewInt(peers[i].Upload + peers[i].Download)
			)

			if consumed.GT(available) {
				n.Log().Info("Peer quota exceeded", "key", peers[i].Key)
				if err := n.RemovePeer(item.Key); err != nil {
					return err
				}
			}
		}
	}
}

func (n *Node) jobUpdateStatus() error {
	n.Log().Info("Starting a job", "name", "update_status", "interval", n.IntervalUpdateStatus())

	t := time.NewTicker(n.IntervalUpdateStatus())
	for ; ; <-t.C {
		if err := n.UpdateNodeStatus(); err != nil {
			return err
		}
	}
}

func (n *Node) jobUpdateSessions() error {
	n.Log().Info("Starting a job", "name", "update_sessions", "interval", n.IntervalUpdateSessions())

	t := time.NewTicker(n.IntervalUpdateSessions())
	for ; ; <-t.C {
		var items []types.Session
		n.Database().Model(
			&types.Session{},
		).Find(&items)

		count := len(items)
		if count > 0 {
			n.Log().Info("Validating the saved sessions", "count", count)
		}

		for i := count - 1; i >= 0; i-- {
			session, err := n.Client().QuerySession(items[i].ID)
			if err != nil {
				return err
			}

			subscription, err := n.Client().QuerySubscription(session.Subscription)
			if err != nil {
				return err
			}

			var (
				removePeer    = false
				removeSession = false
				skipUpdate    = false
			)

			if items[i].Download == session.Bandwidth.Upload.Int64() {
				skipUpdate = true
				if items[i].CreatedAt.Before(session.StatusAt) {
					removePeer = true
				}

				n.Log().Info("Stale peer connection", "session", session.Id,
					"key", items[i].Key, "created_at", items[i].CreatedAt, "status_at", session.StatusAt)
			}
			if !subscription.Status.Equal(hubtypes.StatusActive) {
				removePeer = true
				if subscription.Status.Equal(hubtypes.StatusInactive) {
					removeSession, skipUpdate = true, true
				}

				n.Log().Info("Invalid subscription status", "session", session.Id,
					"key", items[i].Key, "subscription", subscription.Id, "status", subscription.Status)
			}
			if !session.Status.Equal(hubtypes.StatusActive) {
				removePeer = true
				if session.Status.Equal(hubtypes.StatusInactive) {
					removeSession, skipUpdate = true, true
				}

				n.Log().Info("Invalid session status", "session", session.Id,
					"key", items[i].Key, "status", session.Status)
			}

			if removePeer {
				if err := n.RemovePeer(items[i].Key); err != nil {
					return err
				}
			}

			if removeSession {
				n.Database().Model(
					&types.Session{},
				).Where(
					&types.Session{
						ID: items[i].ID,
					},
				).Update(
					"address", "",
				)
			}

			if skipUpdate {
				items = append(items[:i], items[i+1:]...)
			}
		}

		n.Database().Model(
			&types.Session{},
		).Where(
			"address = ?", "",
		).Delete(
			&types.Session{},
		)

		if len(items) == 0 {
			continue
		}

		if err := n.UpdateSessions(items...); err != nil {
			return err
		}
	}
}
