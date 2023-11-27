package consensus

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/inconshreveable/log15"
	"github.com/meterio/meter-pov/block"
)

const (
	OUT_QUEUE_TTL      = time.Second * 5
	REQ_TIMEOUT        = time.Second * 4
	WORKER_CONCURRENCY = 8
)

type OutgoingParcel struct {
	to        *ConsensusPeer
	msg       block.ConsensusMessage
	rawMsg    []byte
	relay     bool
	enqueueAt time.Time
	expireAt  time.Time
}

func (p *OutgoingParcel) Expired() bool {
	return time.Now().After(p.expireAt)
}

type OutgoingQueue struct {
	sync.WaitGroup
	logger  log15.Logger
	queue   chan (*OutgoingParcel)
	clients map[string]*http.Client
}

func NewOutgoingQueue() *OutgoingQueue {
	return &OutgoingQueue{
		logger:  log15.New("pkg", "out"),
		queue:   make(chan (*OutgoingParcel), 2048),
		clients: make(map[string]*http.Client),
	}
}

func (q *OutgoingQueue) Add(to *ConsensusPeer, msg block.ConsensusMessage, rawMsg []byte, relay bool) {
	q.logger.Debug(fmt.Sprintf("add %s msg to out queue", msg.GetType()), "to", to, "len", len(q.queue), "cap", cap(q.queue))
	for len(q.queue) >= cap(q.queue) {
		p := <-q.queue
		q.logger.Info(fmt.Sprintf(`%s msg dropped due to cap ...`, p.msg.GetType()))
	}
	q.queue <- &OutgoingParcel{to: to, msg: msg, rawMsg: rawMsg, relay: relay, enqueueAt: time.Now(), expireAt: time.Now().Add(OUT_QUEUE_TTL)}
}

func (q *OutgoingQueue) Start(ctx context.Context) {
	q.logger.Info(`outgoing queue started`)

	for i := 1; i <= WORKER_CONCURRENCY; i++ {
		worker := NewOutgoingWorker(i)
		q.WaitGroup.Add(1)
		go worker.Run(ctx, q.queue, &q.WaitGroup)
	}
	<-ctx.Done()
	close(q.queue)
	q.WaitGroup.Wait()
}

type outgoingWorker struct {
	logger  log15.Logger
	clients map[string]*http.Client
}

func NewOutgoingWorker(num int) *outgoingWorker {
	return &outgoingWorker{
		logger:  log15.New(), //log15.New("pkg", fmt.Sprintf("w%d", num)),
		clients: make(map[string]*http.Client),
	}
}

func (w *outgoingWorker) Run(ctx context.Context, queue chan *OutgoingParcel, wg *sync.WaitGroup) {
	defer wg.Done()

	for parcel := range queue {
		if parcel.Expired() {
			w.logger.Info(fmt.Sprintf(`outgoing %s msg expired, dropped ...`, parcel.msg.GetType()))
			continue
		}
		ipAddr := parcel.to.IP
		if _, known := w.clients[ipAddr]; !known {
			w.clients[ipAddr] = &http.Client{Timeout: REQ_TIMEOUT}
		}
		client := w.clients[ipAddr]
		url := "http://" + parcel.to.IP + ":8670/pacemaker"

		if parcel.relay {
			w.logger.Debug(fmt.Sprintf(`relay %s`, parcel.msg.GetType()), "to", parcel.to)
		} else {
			w.logger.Info(fmt.Sprintf(`send %s`, parcel.msg.String()), "to", parcel.to)

		}
		res, err := client.Post(url, "application/json", bytes.NewBuffer(parcel.rawMsg))

		// TODO: print response
		if err != nil {
			w.logger.Error(fmt.Sprintf("send msg %s failed", parcel.msg.GetType()), "to", parcel.to, "err", err)
			w.clients[ipAddr] = &http.Client{Timeout: REQ_TIMEOUT}
			continue
		}
		// defer res.Body.Close()
		io.Copy(ioutil.Discard, res.Body)
		res.Body.Close()
	}
}