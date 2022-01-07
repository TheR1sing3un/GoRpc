package codec

import "io"

//封装请求和回复中除了返回值和参数之外的信息
type Header struct {
	//服务名和方法名
	ServiceMethod string
	//序列号,可认为某个请求的id,区分不同请求
	Seq uint64
	//错误信息
	Error string
}

//抽象对消息体进行编解码的接口Codec,为了实现不同的实例
type Codec interface {
	io.Closer
	//读取Header
	ReadHeader(header *Header) error
	//读取消息体
	ReadBody(interface{}) error
	//
	Write(*Header, interface{}) error
}

//抽象Codec的构造函数
type NewCodecFunc func(conn io.ReadWriteCloser) Codec

type Type string

const (
	//Gob协议解析
	GobType Type = "application/gob"
	//Json协议解析
	JsonType Type = "application/json"
)

//一个Type->NewCodecFunc,根据Type类型获取相应构造函数
var NewCodeFuncMap map[Type]NewCodecFunc

func init() {
	NewCodeFuncMap = make(map[Type]NewCodecFunc)
	//将Gob的构造函数添加进去
	NewCodeFuncMap[GobType] = NewGobCodecFunc
}
