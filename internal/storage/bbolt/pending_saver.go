package bbolt

import (
	"cmp"
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"link-checker/internal/model"
)

const (
	DefaultSaveQueueSize = 2048
	DefaultSaveDelay     = 10 * time.Millisecond
)

var ErrSaverClosed = errors.New("saver closed")

type pendingSaverConfig struct {
	QueueSize int
	Delay     time.Duration
}

type saveReq struct {
	links  []model.Link
	respCh chan<- saveResp
}

type saveResp struct {
	id  uint64
	err error
}

type saveBatchFunc func(ctx context.Context, batch [][]model.Link) ([]uint64, error)

type pendingSaver struct {
	saveBatch saveBatchFunc

	queueSize int
	saveDelay time.Duration

	inputCh chan saveReq
	closeCh chan struct{}
	done    chan struct{}
	closeMu sync.Mutex
}

func newPendingSaver(saveBatch saveBatchFunc, cfg pendingSaverConfig) *pendingSaver {
	if saveBatch == nil {
		panic("newSaver: saveBatch cannot be nil")
	}

	s := &pendingSaver{
		saveBatch: saveBatch,

		queueSize: cmp.Or(max(cfg.QueueSize, 0), DefaultSaveQueueSize),
		saveDelay: cmp.Or(max(cfg.Delay, 0), DefaultSaveDelay),

		inputCh: make(chan saveReq),
		closeCh: make(chan struct{}),
		done:    make(chan struct{}),
	}
	go s.serve()
	return s
}

func (s *pendingSaver) Close() error {
	return s.CloseCtx(context.Background())
}

func (s *pendingSaver) CloseCtx(ctx context.Context) error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	// Проверяем, что не закрыты
	select {
	case <-s.closeCh:
		return ErrSaverClosed
	default:
	}

	// Сообщаем serve, что нужно завершиться
	close(s.closeCh)

	// Ждем завершения или отмены контекста
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
	}

	return nil
}

func (s *pendingSaver) Save(ctx context.Context, links []model.Link) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// Ждем пока serve прочтет запрос или отмены контекста
	respCh := make(chan saveResp, 1)
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-s.closeCh:
		return 0, ErrSaverClosed
	case s.inputCh <- saveReq{links: links, respCh: respCh}:
	}

	// ждем ответ или отмены контекста
	var resp saveResp
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case resp = <-respCh:
	}

	return resp.id, resp.err
}

type queue struct {
	batch   [][]model.Link
	respChs []chan<- saveResp
}

func newQueue(n int) *queue {
	return &queue{
		batch:   make([][]model.Link, 0, n),
		respChs: make([]chan<- saveResp, 0, n),
	}
}

func (q *queue) len() int {
	return len(q.batch)
}

func (q *queue) append(req saveReq) {
	q.batch = append(q.batch, req.links)
	q.respChs = append(q.respChs, req.respCh)
}

func (q *queue) reset() {
	q.batch = q.batch[:0]
	q.respChs = q.respChs[:0]
}

func (s *pendingSaver) serve() {
	// Фоновый процесс, который выполняет запись в базу данных.
	// ВАЖНО: Одновременно выполняется только ОДНА задача.
	saveCh := make(chan *queue)
	go func() {
		defer close(s.done)
		for queue := range saveCh {
			s.syncSave(queue)
		}
	}()

	// Канал отложенной записи в базу. Устанавливается по таймеру,
	// после того, как в очередь попал первый запрос.
	var pendingSaveCh chan *queue

	// Таймер обеспечивает задержку записи.
	tm := time.NewTimer(math.MaxInt64)
	tm.Stop()
	defer tm.Stop()

	// Создаем две очереди. В первую пишем, вторую скидываем в базу.
	queue1 := newQueue(s.queueSize)
	queue2 := newQueue(s.queueSize)

	swapQueues := func() {
		tm.Stop()
		pendingSaveCh = nil
		queue1, queue2 = queue2, queue1
		queue1.reset()
	}

	for {
		select {
		case <-s.closeCh:
			// Принудительно сбрасываем очередь.
			if queue1.len() > 0 {
				saveCh <- queue1
			}
			close(saveCh)
			return

		case pendingSaveCh <- queue1:
			swapQueues()

		case <-tm.C:
			pendingSaveCh = saveCh

		case req := <-s.inputCh:
			queue1.append(req)
			switch {
			case queue1.len() >= s.queueSize:
				// При достеженни порога, принудительно сбрасывам очередь.
				// Здесь блокируемся, пока база не будет готова.
				saveCh <- queue1
				swapQueues()

			case queue1.len() == 1:
				tm.Reset(s.saveDelay)
			}
		}
	}
}

// syncSave сохраняет пачку запрос в базу одной транзакцией
func (s *pendingSaver) syncSave(queue *queue) {
	if queue.len() == 0 {
		return
	}

	ids, err := s.saveBatch(context.Background(), queue.batch)

	// Возвращаем ошибку (если есть) инициаторам запросов
	if err != nil {
		for _, respCh := range queue.respChs {
			respCh <- saveResp{
				id:  0,
				err: err,
			}
		}
		return
	}

	// Возвращаем результат инициаторам запросов
	for i, respCh := range queue.respChs {
		respCh <- saveResp{
			id:  ids[i],
			err: nil,
		}
	}
}
