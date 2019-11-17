// Copyright 2019 The Periph Authors. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package waveshare213v2

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"time"

	"periph.io/x/periph/conn"
	"periph.io/x/periph/conn/display"
	"periph.io/x/periph/conn/gpio"
	"periph.io/x/periph/conn/physic"
	"periph.io/x/periph/conn/spi"
	"periph.io/x/periph/devices/ssd1306/image1bit"
	"periph.io/x/periph/host/rpi"
)

// EPD commands
const (
	driverOutputControl            byte = 0x01
	dataEntryModeSetting           byte = 0x11
	swReset                        byte = 0x12
	temperatureSensorControl       byte = 0x18
	masterActivation               byte = 0x20
	displayUpdateControl2          byte = 0x22
	writeRAMBW                     byte = 0x24
	borderWaveformControl          byte = 0x3C
	setRAMXAddressStartEndPosition byte = 0x44
	setRAMYAddressStartEndPosition byte = 0x45
	setRAMXAddressCounter          byte = 0x4E
	setRAMYAddressCounter          byte = 0x4F
)

const (
	displayWidth  = 122
	displayHeight = 250
)

// Dev is an open handle to the display controller.
type Dev struct {
	conn spi.Conn
	dc   gpio.PinOut
	rst  gpio.PinOut
	busy gpio.PinIO
}

// NewSPIHat returns a Dev object that communicates over SPI
// and have the default config for the e-paper hat for Raspberry Pi.
func NewSPIHat(p spi.Port) (*Dev, error) {
	return NewSPI(p, rpi.P1_22, rpi.P1_11, rpi.P1_18)
}

// NewSPI returns a Dev object that communicates over SPI to a e-paper display controller.
func NewSPI(p spi.Port, dc, rst gpio.PinOut, busy gpio.PinIO) (*Dev, error) {
	if err := dc.Out(gpio.Low); err != nil {
		return nil, err
	}
	conn, err := p.Connect(10*physic.MegaHertz, spi.Mode0, 8)
	if err != nil {
		return nil, err
	}

	d := &Dev{conn: conn, dc: dc, rst: rst, busy: busy}
	if err := d.Init(); err != nil {
		return nil, err
	}
	return d, nil
}

// String implements conn.Resource.
func (d *Dev) String() string {
	return fmt.Sprintf("waveshare213v2.Dev{%s, %s, %s}", d.conn, d.dc, d.Bounds().Max)
}

// ColorModel implements display.Drawer.
// It is a one bit color model, as implemented by image1bit.Bit.
func (d *Dev) ColorModel() color.Model {
	return image1bit.BitModel
}

// Bounds implements display.Drawer.
func (d *Dev) Bounds() image.Rectangle {
	return image.Rect(0, 0, displayWidth, displayHeight)
}

// Draw implements display.Drawer.
func (d *Dev) Draw(dstRect image.Rectangle, src image.Image, sp image.Point) error {
	next := image1bit.NewVerticalLSB(image.Rect(0, 0, 128, 250))
	draw.Draw(next, next.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(next, dstRect, src, sp, draw.Src)

	if err := d.sendCommand(writeRAMBW); err != nil {
		return err
	}
	for y := 0; y < next.Rect.Dy(); y++ {
		var byteToSend byte
		for x := 0; x < next.Rect.Dx(); x++ {
			bit := next.BitAt(next.Rect.Dx()-7-x, y)
			if bit {
				byteToSend |= 0x80 >> (uint32(x) % 8)
			}
			if x%8 == 7 {
				if err := d.sendData(byteToSend); err != nil {
					return err
				}
				byteToSend = 0x00
			}
		}
	}
	return d.Update()
}

// Halt implements conn.Resource. It clears the screen content.
func (d *Dev) Halt() error {
	return d.Draw(d.Bounds(), image.White, image.Point{})
}

// Update performs a full display update.
func (d *Dev) Update() error {
	if err := d.sendCommand(displayUpdateControl2, 0xF7); err != nil {
		return err
	}
	if err := d.sendCommand(masterActivation); err != nil {
		return err
	}
	for d.busy.Read() == gpio.High {
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// Init resets and initializes the display.
func (d *Dev) Init() error {
	// HW reset
	if err := d.rst.Out(gpio.High); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	if err := d.rst.Out(gpio.Low); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	if err := d.rst.Out(gpio.High); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	// SW reset
	if err := d.sendCommand(swReset); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)

	// Send initialization code
	if err := d.sendCommand(driverOutputControl, byte((d.Bounds().Dy()-1)&0xFF), byte(((d.Bounds().Dy()-1)>>8)&0xFF), 0x00); err != nil {
		return err
	}
	if err := d.sendCommand(dataEntryModeSetting, 0x01); err != nil {
		return err
	}
	if err := d.sendCommand(setRAMXAddressStartEndPosition, 0x00, 0x0F); err != nil { //0x0F-->(15+1)*8=128
		return err
	}
	if err := d.sendCommand(setRAMYAddressStartEndPosition, 0xF9, 0x00, 0x00, 0x00); err != nil { //0xF9-->(249+1)=250
		return err
	}
	if err := d.sendCommand(borderWaveformControl, 0x01); err != nil {
		return err
	}
	if err := d.sendCommand(temperatureSensorControl, 0x80); err != nil {
		return err
	}
	if err := d.sendCommand(setRAMXAddressCounter, 0x00); err != nil {
		return err
	}
	if err := d.sendCommand(setRAMYAddressCounter, 0xF9, 0x00); err != nil {
		return err
	}

	return nil
}

func (d *Dev) sendCommand(command byte, data ...byte) error {
	if err := d.dc.Out(gpio.Low); err != nil {
		return err
	}
	if err := d.conn.Tx([]byte{command}, nil); err != nil {
		return err
	}
	if len(data) != 0 {
		if err := d.sendData(data...); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dev) sendData(data ...byte) error {
	packets := make([]spi.Packet, len(data))
	for i := range data {
		packets[i] = spi.Packet{W: []byte{data[i]}}
	}
	if err := d.dc.Out(gpio.High); err != nil {
		return err
	}
	return d.conn.TxPackets(packets)
}

var _ display.Drawer = &Dev{}
var _ conn.Resource = &Dev{}
