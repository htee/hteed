package redis

import (
	"errors"

	redigo "github.com/garyburd/redigo/redis"
)

type TransactionConn struct {
	Conn redigo.Conn

	commands int
	values   *[]interface{}
}

func Transaction(c redigo.Conn) *TransactionConn {
	return &TransactionConn{c, 0, nil}
}

func (c *TransactionConn) Close() error {
	return c.Conn.Close()
}

func (c *TransactionConn) Start() error {
	return c.Conn.Send("MULTI")
}

func (c *TransactionConn) Send(commandName string, args ...interface{}) error {
	c.commands++

	return c.Conn.Send(commandName, args...)
}

func (c *TransactionConn) Receive() (interface{}, error) {
	if len(*c.values) < 1 {
		return nil, errors.New("redigo: transaction returned less values than expected")
	}

	value := (*c.values)[0]
	*c.values = (*c.values)[1:]

	return value, nil
}

func (c *TransactionConn) Exec() error {
	c.Conn.Send("EXEC")
	c.Conn.Flush()

	if err := c.expect("OK"); err != nil {
		return err
	}

	for i := 0; i < c.commands; i++ {
		if err := c.expect("QUEUED"); err != nil {
			return err
		}
	}

	var values []interface{}
	var err error

	if values, err = redigo.Values(c.Conn.Receive()); err != nil {
		return err
	}

	c.values = &values

	return nil
}

func (c *TransactionConn) expect(expected string) error {

	if s, err := redigo.String(c.Conn.Receive()); err != nil || s != expected {
		if err != nil {
			return err
		} else {
			return errors.New("redigo: unexpected transaction response: " + s)
		}
	}

	return nil
}
