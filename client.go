package gorpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/TheR1sing3un/gorpc/codec"
	"log"
	"net"
	"sync"
)

//调用结构体
type Call struct {
	//序列号
	Seq uint64
	//方法名,格式: <service>.<method>
	ServiceMethod string
	//参数
	Args interface{}
	//返回值
	Reply interface{}
	//错误
	Error error
	//当该调用完成时的通知chan
	Done chan *Call
}

//当调用结束时会通知调用方
func (call *Call) done() {
	call.Done <- call
}

type Client struct {
	//编解码类
	c codec.Codec
	//请求Option信息
	option *Option
	//发送锁(保证请求都被完整发送)
	sendLock sync.Mutex
	//请求header(多个请求都复用该header)
	header codec.Header
	//加锁保证修改和访问pending和设置变量时没有并发问题
	lock sync.Mutex
	//序列号
	seq uint64
	//存储未处理完的请求
	pending map[uint64]*Call
	//用户调用是否关闭
	closed bool
	//服务端是否通知关闭
	shutdown bool
}

var ErrShutdown = errors.New("conn is shut down")

//主动关闭连接
func (clent *Client) Close() error {
	clent.lock.Lock()
	defer clent.lock.Unlock()
	if clent.closed {
		//如果已经关闭
		return ErrShutdown
	}
	clent.closed = true
	return clent.c.Close()
}

//判断客户端目前是否可用
func (client *Client) IsAvailable() bool {
	client.lock.Lock()
	defer client.lock.Unlock()
	return !client.shutdown && !client.closed
}

//注册调用方法,返回调用对象的序列号
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.lock.Lock()
	defer client.lock.Unlock()
	//当客户端已关闭时
	if client.closed || client.shutdown {
		return 0, ErrShutdown
	}
	//将调用序列号设为客户端的序列号
	call.Seq = client.seq
	//将该seq->call加入到pending
	client.pending[call.Seq] = call
	//序列号自增
	client.seq++
	return call.Seq, nil
}

//删除调用方法
func (client *Client) removeCall(seq uint64) *Call {
	client.lock.Lock()
	defer client.lock.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

//当客户端或者服务端发生故障时,调用该函数将shutdown改成true并且通知所有的pending中的call
func (client *Client) terminateCalls(err error) {
	client.sendLock.Lock()
	defer client.sendLock.Unlock()
	client.lock.Lock()
	defer client.lock.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

//接受响应
func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		//从客户端的codec读取请求Header
		if err = client.c.ReadHeader(&h); err != nil {
			//报错退出循环
			break
		}
		call := client.removeCall(h.Seq)
		switch {
		//当根据seq获取的调用实例为空
		case call == nil:
			err = client.c.ReadBody(nil)
		case h.Error != "":
			//当header中的错误信息不为空
			call.Error = fmt.Errorf(h.Error)
			err = client.c.ReadBody(nil)
			//调用结束
			call.done()
		default:
			//读取Body然后赋值给call.Reply
			err = client.c.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	//有错误时
	client.terminateCalls(err)
}

//初始化Client,发送option完成协议交换,然后创建一个子协程来接收响应
func NewClient(conn net.Conn, option *Option) (*Client, error) {
	//根据CodecType获取对应协议的构造方法
	codecFunc := codec.NewCodeFuncMap[option.CodecType]
	if codecFunc == nil {
		//为空则该协议不支持
		err := fmt.Errorf("invalid codec type %s", option.CodecType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	//发送options到服务端来确定协议
	if err := json.NewEncoder(conn).Encode(option); err != nil {
		log.Println("rpc client: options error:", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCodec(codecFunc(conn), option), nil
}

//根据codec和option来创建客户端
func newClientCodec(c codec.Codec, option *Option) *Client {
	client := &Client{
		seq:     1,
		c:       c,
		option:  option,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

//Dial方法,使用户传入服务端地址,创建client实例
func Dial(network string, address string, options ...*Option) (client *Client, err error) {
	//解析传入的...options
	option, err := parseOptions(options...)
	if err != nil {
		return nil, err
	}
	//与服务端获取连接
	conn, err := net.Dial(network, address)
	//最后如果返回的client为空,此时直接关闭连接
	defer func() {
		if client == nil {
			conn.Close()
		}
	}()
	return NewClient(conn, option)
}

//解析传入的Option
func parseOptions(options ...*Option) (*Option, error) {
	//如果没有传option,则使用默认option
	if len(options) == 0 || options[0] == nil {
		return DefaultOption, nil
	}
	if len(options) != 1 {
		//若传进多个option
		return nil, errors.New("number of options is more than 1")
	}
	option := options[0]
	//给magicNumber赋值
	option.MagicNumber = DefaultOption.MagicNumber
	//如果传入的option没有codecType则给默认的type
	if option.CodecType == "" {
		option.CodecType = DefaultOption.CodecType
	}
	return option, nil
}

//发送调用信息
func (client *Client) send(call *Call) {
	//发送加锁,保证发送完整的请求
	client.sendLock.Lock()
	defer client.sendLock.Unlock()

	//去注册该调用
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		//结束调用,给调用方发消息(by chan)
		call.done()
		return
	}

	//准备请求头
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	//编码并发送
	if err := client.c.Write(&client.header, call.Args); err != nil {
		//报错则将该调用删去
		call := client.removeCall(seq)
		if call != nil {
			call.Error = err
			//结束调用,给调用方发消息(by chan)
			call.done()
		}
	}
}

func (client *Client) Go(serviceMethod string, args interface{}, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		//还未完成
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	//调用
	client.send(call)
	return call
}

func (client *Client) Call(serviceMethod string, args, reply interface{}) error {
	//等待调用完成通过chan将call传递过来
	call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}
