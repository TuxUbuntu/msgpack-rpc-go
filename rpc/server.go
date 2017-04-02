package rpc

import (
	"fmt"
	"io"
	"log"
	msgpack "github.com/msgpack/msgpack-go"
	"net"
	"os"
	"reflect"
	"errors"
)


type Server struct {
	resolver     FunctionResolver
	log          *log.Logger
	listeners    []net.Listener
	autoCoercing bool
	lchan        chan int
}

// Goes into the event loop to get ready to serve.
func (self *Server) Run() *Server {
	lchan := make(chan int)
	for _, listener := range self.listeners {
		go (func(listener net.Listener) {
			for {
				conn, err := listener.Accept()
				if err != nil {
					self.log.Println(err)
					continue
				}
				if self.lchan == nil {
					conn.Close()
					break
				}
				session := NewSession(conn, self.autoCoercing)
				go session.Run(self.resolver)
				// self.log.Println(msg)
			}
		})(listener)
	}
	self.lchan = lchan
	<-lchan
	for _, listener := range self.listeners {
		listener.Close()
	}
	return self
}

// Lets the server quit the event loop
func (self *Server) Stop() *Server {
	if self.lchan != nil {
		lchan := self.lchan
		self.lchan = nil
		lchan <- 1
	}
	return self
}

// Listenes on the specified transport.  A single server can listen on the
// multiple ports.
func (self *Server) Listen(listener net.Listener) *Server {
	self.listeners = append(self.listeners, listener)
	return self
}


// integerPromote determines if we can promote v to dType, and if so, return the promoted value.
// This is needed because msgpack always encodes values as the minimum sized int that can hold them.
func integerPromote(dType reflect.Type, v reflect.Value) (reflect.Value, bool) {

	vt := v.Type()
	dsz := dType.Size()
	vtsz := vt.Size()

	if isIntType(dType) && isIntType(vt) && vtsz <= dsz {
		pv := reflect.New(dType).Elem()
		pv.SetInt(v.Int())
		return pv, true
	}

	if isUintType(dType) && isUintType(vt) && vtsz <= dsz {
		pv := reflect.New(dType).Elem()
		pv.SetUint(v.Uint())
		return pv, true
	}

	if isIntType(dType) && isUintType(vt) && vtsz <= dsz {
		pv := reflect.New(dType).Elem()
		pv.SetInt(int64(v.Uint()))
		return pv, true
	}

	if isUintType(dType) && isIntType(vt) && vtsz <= dsz {
		pv := reflect.New(dType).Elem()
		pv.SetUint(uint64(v.Int()))
		return pv, true
	}

	return v, false
}

type kinder interface {
	Kind() reflect.Kind
}

func isIntType(t kinder) bool {
	return t.Kind() == reflect.Int ||
		t.Kind() == reflect.Int8 ||
		t.Kind() == reflect.Int16 ||
		t.Kind() == reflect.Int32 ||
		t.Kind() == reflect.Int64
}

func isUintType(t kinder) bool {
	return t.Kind() == reflect.Uint ||
		t.Kind() == reflect.Uint8 ||
		t.Kind() == reflect.Uint16 ||
		t.Kind() == reflect.Uint32 ||
		t.Kind() == reflect.Uint64
}
// Creates a new Server instance. raw bytesc are automatically converted into
// strings if autoCoercing is enabled.
func NewServer(resolver FunctionResolver, autoCoercing bool, _log *log.Logger) *Server {
	if _log == nil {
		_log = log.New(os.Stderr, "msgpack: ", log.Ldate|log.Ltime)
	}
	return &Server{resolver, _log, make([]net.Listener, 0), autoCoercing, nil}
}

// This is a low-level function that is not supposed to be called directly
// by the user.  Change this if the MessagePack protocol is updated.
func HandleRPCRequest(req reflect.Value) (int, string, []reflect.Value, error) {
	for{
		_req, ok := req.Interface().([]reflect.Value)
		if !ok {
			break;
		}
		if len(_req) != 4 {
			break;
		}
		msgType := _req[0]
		typeOk := msgType.Kind() == reflect.Int || msgType.Kind() == reflect.Int8 || msgType.Kind() == reflect.Int16 || msgType.Kind() == reflect.Int32 || msgType.Kind() == reflect.Int64
		if !typeOk {
			fmt.Println("error: typeOk")
			break;
		}
		msgId := _req[1]
		idOk := msgId.Kind() == reflect.Int || msgId.Kind() == reflect.Int8 || msgId.Kind() == reflect.Int16 || msgId.Kind() == reflect.Int32 || msgId.Kind() == reflect.Int64
		if !idOk {
			fmt.Println("error: idOk")
			break;
		}
		_funcName := _req[2]
		funcOk := _funcName.Kind() == reflect.Array || _funcName.Kind() == reflect.Slice
		if !funcOk {
			fmt.Println("error: funcOk", _funcName)
			break;
		}
		funcName, ok := _funcName.Interface().([]uint8)
		if !ok {
			fmt.Println("error: funcName")
			break;
		}
		if msgType.Int() != REQUEST {
			fmt.Println("error: msgType", msgType.Int())
			break;
		}
		_arguments := _req[3]
		var arguments []reflect.Value
		if _arguments.Kind() == reflect.Array || _arguments.Kind() == reflect.Slice {
			elemType := _req[3].Type().Elem()
			_elemType := elemType
			ok := _elemType.Kind() == reflect.Uint || _elemType.Kind() == reflect.Uint8 || _elemType.Kind() == reflect.Uint16 || _elemType.Kind() == reflect.Uint32 || _elemType.Kind() == reflect.Uint64 || _elemType.Kind() == reflect.Uintptr
			if !ok || _elemType.Kind() != reflect.Uint8 {
				arguments, ok = _arguments.Interface().([]reflect.Value)
			} else {
				arguments = []reflect.Value{reflect.ValueOf(string(_req[3].Interface().([]byte)))}
			}
		} else {
			arguments = []reflect.Value{_req[3]}
		}
		return int(msgId.Int()), string(funcName), arguments, nil
	}
	return 0, "", nil, errors.New("Invalid message format server")
}

// This is a low-level function that is not supposed to be called directly
// by the user.  Change this if the MessagePack protocol is updated.
func SendResponseMessage(writer io.Writer, msgId int, value reflect.Value) error {
	_, err := writer.Write([]byte{0x94})
	if err != nil {
		return err
	}
	_, err = msgpack.PackInt8(writer, RESPONSE)
	if err != nil {
		return err
	}
	_, err = msgpack.PackInt(writer, msgId)
	if err != nil {
		return err
	}
	_, err = msgpack.PackNil(writer)
	if err != nil {
		return err
	}
	_, err = msgpack.PackValue(writer, value)
	return err
}

// This is a low-level function that is not supposed to be called directly
// by the user.  Change this if the MessagePack protocol is updated.
func SendErrorResponseMessage(writer io.Writer, msgId int, errMsg string) error {
	_, err := writer.Write([]byte{0x94})
	if err != nil {
		return err
	}
	_, err = msgpack.PackInt8(writer, RESPONSE)
	if err != nil {
		return err
	}
	_, err = msgpack.PackInt(writer, msgId)
	if err != nil {
		return err
	}
	_, err = msgpack.PackBytes(writer, []byte(errMsg))
	if err != nil {
		return err
	}
	_, err = msgpack.PackNil(writer)
	return err
}
