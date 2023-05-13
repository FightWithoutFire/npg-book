package housework

import (
	"context"
	"sync"
)

type Rosie struct {
	mu     sync.Mutex
	chores []*Chore
}

func (r *Rosie) mustEmbedUnimplementedRobotMaidServer() {
	//TODO implement me
	panic("implement me")
}

func (r *Rosie) Add(_ context.Context, chores *Chores) (*Response, error) {

	r.mu.Lock()
	defer r.mu.Unlock()

	r.chores = append(r.chores, chores.Chores...)

	return &Response{Message: "ok"}, nil
}

func (r *Rosie) Complete(_ context.Context, request *CompleteRequest) (*Response, error) {

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.chores == nil || request.ChoreNumber < 1 || int(request.ChoreNumber) > len(r.chores) {
		return &Response{Message: "invalid chore number"}, nil
	}

	r.chores[request.ChoreNumber-1].Complete = true

	return &Response{Message: "ok"}, nil
}

func (r *Rosie) List(_ context.Context, _ *Empty) (*Chores, error) {

	r.mu.Lock()
	defer r.mu.Unlock()

	return &Chores{Chores: r.chores}, nil
}

type RobotMaidService struct {
	Add      func(context.Context, *Chores) (*Response, error)
	Complete func(context.Context, *CompleteRequest) (*Response, error)
	List     func(context.Context, *Empty) (*Chores, error)
}

func (r *Rosie) Service() *RobotMaidService {
	return &RobotMaidService{
		Add:      r.Add,
		Complete: r.Complete,
		List:     r.List,
	}
}
