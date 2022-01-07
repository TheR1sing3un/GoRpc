package gorpc

import (
	"log"
	"reflect"
	"testing"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

func (f *Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func TestNewService(t *testing.T) {
	var foo Foo
	s := newService(&foo)
	mType := s.method["Sum"]
	argv := mType.newArgv()
	reply := mType.newReply()
	argv.Set(reflect.ValueOf(Args{Num1: 1, Num2: 2}))
	err := s.call(mType, argv, reply)
	if err != nil {
		log.Panicln("call error:", err)
	}
	log.Println("reply : ", reply.Elem())
}
