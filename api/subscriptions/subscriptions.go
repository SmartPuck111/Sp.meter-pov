// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package subscriptions

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/meterio/meter-pov/api/utils"
	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/chain"
	"github.com/meterio/meter-pov/meter"
	"github.com/pkg/errors"
)

type Subscriptions struct {
	backtraceLimit uint32
	chain          *chain.Chain
	upgrader       *websocket.Upgrader
	done           chan struct{}
	wg             sync.WaitGroup
	logger         *slog.Logger
}

type msgReader interface {
	Read() (msgs []interface{}, hasMore bool, err error)
}

func New(chain *chain.Chain, allowedOrigins []string, backtraceLimit uint32) *Subscriptions {
	return &Subscriptions{
		logger:         slog.With("api", "sub"),
		backtraceLimit: backtraceLimit,
		chain:          chain,
		upgrader: &websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}
				for _, allowedOrigin := range allowedOrigins {
					if allowedOrigin == origin || allowedOrigin == "*" {
						return true
					}
				}
				return false
			},
		},
		done: make(chan struct{}),
	}
}

func (s *Subscriptions) handleBlockReader(w http.ResponseWriter, req *http.Request) (*blockReader, error) {
	position, err := s.parsePosition(req.URL.Query().Get("pos"))
	if err != nil {
		return nil, err
	}
	return newBlockReader(s.chain, position), nil
}

func (s *Subscriptions) handleEventReader(w http.ResponseWriter, req *http.Request) (*eventReader, error) {
	position, err := s.parsePosition(req.URL.Query().Get("pos"))
	if err != nil {
		return nil, err
	}
	address, err := parseAddress(req.URL.Query().Get("addr"))
	if err != nil {
		return nil, utils.BadRequest(errors.WithMessage(err, "addr"))
	}
	t0, err := parseTopic(req.URL.Query().Get("t0"))
	if err != nil {
		return nil, utils.BadRequest(errors.WithMessage(err, "t0"))
	}
	t1, err := parseTopic(req.URL.Query().Get("t1"))
	if err != nil {
		return nil, utils.BadRequest(errors.WithMessage(err, "t1"))
	}
	t2, err := parseTopic(req.URL.Query().Get("t2"))
	if err != nil {
		return nil, utils.BadRequest(errors.WithMessage(err, "t2"))
	}
	t3, err := parseTopic(req.URL.Query().Get("t3"))
	if err != nil {
		return nil, utils.BadRequest(errors.WithMessage(err, "t3"))
	}
	t4, err := parseTopic(req.URL.Query().Get("t4"))
	if err != nil {
		return nil, utils.BadRequest(errors.WithMessage(err, "t4"))
	}
	eventFilter := &EventFilter{
		Address: address,
		Topic0:  t0,
		Topic1:  t1,
		Topic2:  t2,
		Topic3:  t3,
		Topic4:  t4,
	}
	return newEventReader(s.chain, position, eventFilter), nil
}

func (s *Subscriptions) handleTransferReader(w http.ResponseWriter, req *http.Request) (*transferReader, error) {
	position, err := s.parsePosition(req.URL.Query().Get("pos"))
	if err != nil {
		return nil, err
	}
	txOrigin, err := parseAddress(req.URL.Query().Get("txOrigin"))
	if err != nil {
		return nil, utils.BadRequest(errors.WithMessage(err, "txOrigin"))
	}
	sender, err := parseAddress(req.URL.Query().Get("sender"))
	if err != nil {
		return nil, utils.BadRequest(errors.WithMessage(err, "sender"))
	}
	recipient, err := parseAddress(req.URL.Query().Get("recipient"))
	if err != nil {
		return nil, utils.BadRequest(errors.WithMessage(err, "recipient"))
	}
	transferFilter := &TransferFilter{
		TxOrigin:  txOrigin,
		Sender:    sender,
		Recipient: recipient,
	}
	return newTransferReader(s.chain, position, transferFilter), nil
}

func (s *Subscriptions) handleBeatReader(w http.ResponseWriter, req *http.Request) (*beatReader, error) {
	position, err := s.parsePosition(req.URL.Query().Get("pos"))
	if err != nil {
		return nil, err
	}
	return newBeatReader(s.chain, position), nil
}

func (s *Subscriptions) handleSubject(w http.ResponseWriter, req *http.Request) error {
	s.wg.Add(1)
	defer s.wg.Done()

	var (
		reader msgReader
		err    error
	)
	switch mux.Vars(req)["subject"] {
	case "block":
		if reader, err = s.handleBlockReader(w, req); err != nil {
			return err
		}
	case "event":
		if reader, err = s.handleEventReader(w, req); err != nil {
			return err
		}
	case "transfer":
		if reader, err = s.handleTransferReader(w, req); err != nil {
			return err
		}
	case "beat":
		if reader, err = s.handleBeatReader(w, req); err != nil {
			return err
		}

	default:
		return utils.HTTPError(errors.New("not found"), http.StatusNotFound)
	}

	conn, err := s.upgrader.Upgrade(w, req, nil)
	// since the conn is hijacked here, no error should be returned in lines below
	if err != nil {
		s.logger.Debug("upgrade to websocket", "err", err)
		return nil
	}

	defer func() {
		if err := conn.Close(); err != nil {
			s.logger.Debug("close websocket", "err", err)
		}
	}()

	var closeMsg []byte
	if err := s.pipe(conn, reader); err != nil {
		closeMsg = websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())
	} else {
		closeMsg = websocket.FormatCloseMessage(websocket.CloseGoingAway, "")
	}

	if err := conn.WriteMessage(websocket.CloseMessage, closeMsg); err != nil {
		s.logger.Debug("write close message", "err", err)
	}
	return nil
}

func (s *Subscriptions) pipe(conn *websocket.Conn, reader msgReader) error {
	closed := make(chan struct{})
	// start read loop to handle close event
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				s.logger.Debug("websocket read err", "err", err)
				close(closed)
				break
			}
		}
	}()
	ticker := s.chain.NewTicker()
	for {
		msgs, hasMore, err := reader.Read()
		if err != nil {
			return err
		}
		for _, msg := range msgs {
			if err := conn.WriteJSON(msg); err != nil {
				return err
			}
		}
		if !hasMore {
			select {
			case <-s.done:
				return nil
			case <-closed:
				return nil
			case <-ticker.C():
			}
		} else {
			select {
			case <-s.done:
				return nil
			case <-closed:
				return nil
			default:
			}
		}
	}
}

func (s *Subscriptions) parsePosition(posStr string) (meter.Bytes32, error) {
	bestID := s.chain.BestBlock().ID()
	if posStr == "" {
		return bestID, nil
	}
	pos, err := meter.ParseBytes32(posStr)
	if err != nil {
		return meter.Bytes32{}, utils.BadRequest(errors.WithMessage(err, "pos"))
	}
	if block.Number(bestID)-block.Number(pos) > s.backtraceLimit {
		return meter.Bytes32{}, utils.Forbidden(errors.New("pos: backtrace limit exceeded"))
	}
	return pos, nil
}

func parseTopic(t string) (*meter.Bytes32, error) {
	if t == "" {
		return nil, nil
	}
	topic, err := meter.ParseBytes32(t)
	if err != nil {
		return nil, err
	}
	return &topic, nil
}

func parseAddress(addr string) (*meter.Address, error) {
	if addr == "" {
		return nil, nil
	}
	address, err := meter.ParseAddress(addr)
	if err != nil {
		return nil, err
	}
	return &address, nil
}

func (s *Subscriptions) Close() {
	close(s.done)
	s.wg.Wait()
}

func (s *Subscriptions) Mount(root *mux.Router, pathPrefix string) {
	sub := root.PathPrefix(pathPrefix).Subrouter()

	sub.Path("/{subject}").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(s.handleSubject))
}
