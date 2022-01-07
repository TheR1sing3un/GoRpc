package gorpc

import (
	"encoding/json"
	"fmt"
	"github.com/TheR1sing3un/gorpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
)

const MagicNumber = 0x3bef5c

//对协议协商的封装
type Option struct {
	//用于标记不同的rpc请求
	MagicNumber int
	//协议类型
	CodecType codec.Type
}

//默认Option构造
var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

//默认Server实例
var DefaultServer = NewServer()

//实现Accept方法
func (server *Server) Accept(lis net.Listener) {
	//for循环不断处理Accept的连接,并且使用协程处理
	for {
		//从listener接收连接
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		//协程处理每个连接
		go server.ServeConn(conn)
	}
}

//默认Accept方法,使用默认实例
func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	//最后关闭连接
	defer func() {
		_ = conn.Close()
	}()
	var opt Option
	//使用Json格式解析conn,并赋值给opt
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error:", err)
		return
	}
	//验证MagicNumber(传来的是否和本机的相等)
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	//根据opt中传来的CodecType来获取到构造方法
	newCodecFunc := codec.NewCodeFuncMap[opt.CodecType]
	if newCodecFunc == nil {
		log.Printf("rpc server: invalid codec type %x", opt.CodecType)
		return
	}
	//返回该构造方法使用该连接构造出来的Codec
	server.serveCodec(newCodecFunc(conn))
}

var invalidRequest = struct{}{}

//根据Codec来处理
func (server *Server) serveCodec(codec codec.Codec) {
	//发送消息的锁,确保并发下可以依次回复,避免多个回复报文交织在一起导致客户端无法解析
	sendLock := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	//循环等待请求发送过来
	for {
		req, err := server.readRequest(codec)
		if err != nil {
			if req == nil {
				//读取请求错误而且返回为空
				break
			}
			//读取请求错误但是返回不为空,将header放入错误信息
			req.h.Error = err.Error()
			//发送返回消息
			server.sendResponse(codec, req.h, invalidRequest, sendLock)
			continue
		}
		//读取了一个请求后,waitGroup+1,等该请求被处理完之后再Done进行-1
		wg.Add(1)
		go server.handleRequest(codec, req, sendLock, wg)
	}
	//解析出错时,错误的请求在这里wait等待其他请求处理完
	wg.Wait()
	_ = codec.Close()
}

//每个请求的封装
type request struct {
	//请求Header
	h *codec.Header
	//参数值
	argv reflect.Value
	//返回值
	replyv reflect.Value
}

//读取请求的Header
func (server *Server) readRequestHeader(c codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := c.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

//读取请求
func (server *Server) readRequest(c codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(c)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	//TODO 这里应该需要判断argv的类型
	//day1,先假设参数是string类型
	req.argv = reflect.New(reflect.TypeOf(""))
	if err = c.ReadBody(req.argv.Interface()); err != nil {
		//从argv中解析出数据
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil
}

//返回响应
func (server *Server) sendResponse(c codec.Codec, h *codec.Header, body interface{}, sendLock *sync.Mutex) {
	sendLock.Lock()
	defer sendLock.Unlock()
	//加密写消息
	if err := c.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

//处理请求
func (server *Server) handleRequest(c codec.Codec, req *request, sendLock *sync.Mutex, wg *sync.WaitGroup) {
	//TODO 这里应该调用注册的rpc方法,然后得到正确的replyv
	//day1 只做打印argv和返回hello
	//处理完请求,Done使计数器-1
	defer wg.Done()
	//打印header和argv
	log.Println(req.h, req.argv.Elem())
	//将返回值设为header的序列号
	req.replyv = reflect.ValueOf(fmt.Sprintf("gorpc resp %d", req.h.Seq))
	//发送响应
	server.sendResponse(c, req.h, req.replyv.Interface(), sendLock)
}
