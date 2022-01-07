package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

//Gob协议的编码解码结构
type GobCodec struct {
	//链接实例
	conn io.ReadWriteCloser
	//防阻塞,带缓冲的Writer
	buf *bufio.Writer
	//解码器
	dec *gob.Decoder
	//编码器
	enc *gob.Encoder
}

//构造函数
func NewGobCodecFunc(conn io.ReadWriteCloser) Codec {
	//根据连接创建Writer
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

//实现Codec接口中的ReadHeader方法
func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

//
func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		//刷出缓存区
		_ = c.buf.Flush()
		//如果有err,那么关闭连接
		if err != nil {
			_ = c.Close()
		}
	}()
	//对Header进行加密
	if err := c.enc.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}
	//对Body加密
	if err := c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}
	return nil
}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}
