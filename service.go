package gorpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

//rpc调用类型封装结构体, func (t *T) MethodName(argType T1,replyType *T2) error
type methodType struct {
	//方法本身
	method reflect.Method
	//第一个参数类型
	ArgType reflect.Type
	//第二个参数类型
	ReplyType reflect.Type
	//方法次数
	numCalls uint64
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

//获取arg的值
func (m *methodType) newArgv() reflect.Value {
	//第一个参数值
	var argv reflect.Value
	//如果arg的类型为指针
	if m.ArgType.Kind() == reflect.Ptr {
		//reflect.New返回的指针传给argv,
		argv = reflect.New(m.ArgType.Elem())
	} else {
		//为引用的话,将reflect.New返回的指针转成实际的引用值
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

//获取reply的值
func (m *methodType) newReply() reflect.Value {
	//reply一定是指针类型
	reply := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		//如果是map,需要实例化
		reply.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		//同理
		reply.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return reply
}

//结构体映射成服务service
type service struct {
	//结构体名称
	name string
	//结构体类型
	typ reflect.Type
	//实例
	instance reflect.Value
	//存储结构体的方法名->方法
	method map[string]*methodType
}

//根据结构体实例实例化service
func newService(structInstance interface{}) *service {
	s := new(service)
	s.instance = reflect.ValueOf(structInstance)
	s.name = reflect.Indirect(s.instance).Type().Name()
	s.typ = reflect.TypeOf(structInstance)
	//判断该结构体是否合法
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid server name", s.name)
	}
	//注册方法
	s.registerMethods()
	return s
}

//将方法注册进去
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		//获取方法
		method := s.typ.Method(i)
		mType := method.Type
		//判断是否有三个入参(实例本身,入参,指针类型的返回值),是否有一个返回值(也就是error)
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		//判断返回值是否是error类型
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		//获取两个参数
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

//判断该类型是否暴露
func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

//调用方法
func (s *service) call(m *methodType, argv, reply reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	//根据method获取func
	f := m.method.Func
	//调用方法,获取返回值
	returnValues := f.Call([]reflect.Value{s.instance, argv, reply})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
