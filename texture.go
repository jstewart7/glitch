package glitch

import (
	"runtime"
	"github.com/faiface/mainthread"
	"github.com/jstewart7/gl"
)

type Texture struct {
	texture gl.Texture
	width, height int
}

func NewTexture(width, height int, pixels []uint8) *Texture {
	t := &Texture{
		width: width,
		height: height,
	}

	mainthread.Call(func() {
		t.texture = gl.CreateTexture()
		gl.BindTexture(gl.TEXTURE_2D, t.texture)

		gl.TexImage2D(gl.TEXTURE_2D, 0, width, height, gl.RGBA, gl.UNSIGNED_BYTE, pixels)

		// TODO - webgl doesn't support CLAMP_TO_BORDER
		// GL_CLAMP_TO_EDGE: The coordinate will simply be clamped between 0 and 1.
		// GL_CLAMP_TO_BORDER: The coordinates that fall outside the range will be given a specified border color.

		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)

		// TODO - pass smooth in as a parameter
		smooth := false
		if smooth {
			gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
			gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
		} else {
			gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
			gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
		}
	})

	runtime.SetFinalizer(t, (*Texture).delete)

	return t
}

func (t *Texture) Bind() {
	mainthread.Call(func() {
		gl.BindTexture(gl.TEXTURE_2D, t.texture)
	})
}

func (t *Texture) delete() {
	mainthread.CallNonBlock(func() {
		gl.DeleteTexture(t.texture)
	})
}
