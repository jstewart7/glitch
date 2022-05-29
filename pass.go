package glitch

import (
	// "github.com/faiface/mainthread"
	// "github.com/unitoftime/gl"
	// "sort"
)

type Target interface {
	// TODO - Should this be differentiated from being a source Vs a target binding. For example, I'm using this now to bind the target that we draw to. But If I want to have another function on frambuffers to use them as image texture inputs, what would that API be called?
	Bind()
}

// https://realtimecollisiondetection.net/blog/?p=86
// Sort by:
// - Front-to-back vs Back-to-front (single bit)
// - Depth bits
// - Material / Uniforms / Textures
// - Sort by: x, y, z, depth?
type drawCommand struct {
	command uint64
	mesh *Mesh
	matrix Mat4
	mask RGBA
	material Material
}

// This is essentially a generalized 2D render pass
type RenderPass struct {
	shader *Shader
	texture *Texture
	uniforms map[string]interface{}
	buffer *BufferPool
	commands [][]drawCommand
	currentLayer uint8

	dirty bool // Indicates if we need to re-draw to the buffers
}

const DefaultLayer uint8 = 127

func NewRenderPass(shader *Shader) *RenderPass {
	defaultBatchSize := 100000
	return &RenderPass{
		shader: shader,
		texture: nil,
		uniforms: make(map[string]interface{}),
		buffer: NewBufferPool(shader, defaultBatchSize),
		commands: make([][]drawCommand, 256), // TODO - hardcoding from sizeof(uint8)
		currentLayer: DefaultLayer,
		dirty: true,
	}
}

func (r *RenderPass) Clear() {
	r.dirty = true
	// Clear stuff
	r.buffer.Clear()
	// r.commands = r.commands[:0]
	for l := range r.commands {
		r.commands[l] = r.commands[l][:0]
	}
}

// TODO - I think I could use a linked list of layers and just use an int here
func (r *RenderPass) SetLayer(layer uint8) {
	r.currentLayer = layer
}

// TODO - Mat?
func (r *RenderPass) Draw(target Target) {
	// Bind render target
	target.Bind()

	// TODO - Hardware depth testing
	// mainthread.Call(func() {
	// 	//https://gamedev.stackexchange.com/questions/134809/how-do-i-sort-with-both-depth-and-y-axis-in-opengl
	// 	// Do I need? glEnable(GL_ALPHA_TEST); glAlphaFunc(GL_GREATER, 0.9f); - maybe prevents "discard;" in frag shader
	// 	gl.Enable(gl.DEPTH_TEST)
	// })

	// TODO - Software sorting
	// sort.Slice(r.commands, func(i, j int) bool {
	// 	// return r.commands[i].matrix[i4_3_0] < r.commands[j].matrix[i4_3_0] // Sort by x
	// 	// return r.commands[i].matrix[i4_3_1] < r.commands[j].matrix[i4_3_1] // Sort by y
	// 	// return r.commands[i].matrix[i4_3_2] < r.commands[j].matrix[i4_3_2] // Sort by z
	// 	return r.commands[i].command < r.commands[j].command
	// })

	r.shader.Bind()
	for k,v := range r.uniforms {
		ok := r.shader.SetUniform(k, v)
		if !ok {
			panic("Error setting uniform - todo decrease this to log")
		}
	}

	if r.dirty {
		r.dirty = false

		destBuffs := make([]any, len(r.shader.attrFmt))
		for i, attr := range r.shader.attrFmt {
			destBuffs[i] = attr.GetBuffer()
		}

		for l := len(r.commands)-1; l >= 0; l-- { // Reverse order so that layer 0 is drawn last
			for _, c := range r.commands[l] {
				numVerts := len(c.mesh.positions)

				r.buffer.Reserve(c.material, c.mesh.indices, numVerts, destBuffs)

				// TODO If large enough mesh, then don't do matrix transformation, just apply the model matrix to the buffer in the buffer pool

				// Append all mesh buffers to shader buffers
				for bufIdx, attr := range r.shader.attrFmt {
					// TODO - I'm not sure of a good way to break up this switch statement
					switch attr.Swizzle {
						// Positions
					case PositionXY:
						posBuf := *(destBuffs[bufIdx]).(*[]Vec2)
						for i := range c.mesh.positions {
							vec := c.matrix.Apply(c.mesh.positions[i])
							posBuf[i] = *(*Vec2)(vec[:2])
						}

					case PositionXYZ:
						posBuf := *(destBuffs[bufIdx]).(*[]Vec3)
						for i := range c.mesh.positions {
							vec := c.matrix.Apply(c.mesh.positions[i])
							posBuf[i] = vec
						}

						// Colors
					case ColorR:
						colBuf := *(destBuffs[bufIdx]).(*[]float32)
						for i := range c.mesh.colors {
							colBuf[i] = c.mesh.colors[i][0] * c.mask.R
						}
					case ColorRG:
						colBuf := *(destBuffs[bufIdx]).(*[]Vec2)
						for i := range c.mesh.colors {
							colBuf[i] = Vec2{
								c.mesh.colors[i][0] * c.mask.R,
								c.mesh.colors[i][1] * c.mask.G,
							}
						}
					case ColorRGB:
						colBuf := *(destBuffs[bufIdx]).(*[]Vec3)
						for i := range c.mesh.colors {
							colBuf[i] = Vec3{
								c.mesh.colors[i][0] * c.mask.R,
								c.mesh.colors[i][1] * c.mask.G,
								c.mesh.colors[i][2] * c.mask.B,
							}
						}
					case ColorRGBA:
						colBuf := *(destBuffs[bufIdx]).(*[]Vec4)
						for i := range c.mesh.colors {
							colBuf[i] = Vec4{
								c.mesh.colors[i][0] * c.mask.R,
								c.mesh.colors[i][1] * c.mask.G,
								c.mesh.colors[i][2] * c.mask.B,
								c.mesh.colors[i][3] * c.mask.A,
							}
						}

					case TexCoordXY:
						texBuf := *(destBuffs[bufIdx]).(*[]Vec2)
						for i := range c.mesh.texCoords {
							texBuf[i] = c.mesh.texCoords[i]
						}
					}
				}

				//================================================================================
				// TODO The hardcoding is a bit slower. Keeping it around in case I want to do some performance analysis
				// Notes: Ran gophermark with 1000000 gophers.
				// - Hardcoded: ~ 120 to 125 ms range
				// - Switch Statement: ~ 125 to 130 ms range
				// - Switch Statement (with shader changed to use vec2s for position): ~ 122 to 127 ms range
				// work and append
				// 	posBuf := *(destBuffs[0]).(*[]Vec3)
				// 	for i := range c.mesh.positions {
				// 		vec := c.matrix.Apply(c.mesh.positions[i])
				// 		posBuf[i] = vec
				// 	}

				// 	colBuf := *(destBuffs[1]).(*[]Vec4)
				// 	for i := range c.mesh.colors {
				// 		colBuf[i] = Vec4{
				// 			c.mesh.colors[i][0] * c.mask.R,
				// 			c.mesh.colors[i][1] * c.mask.G,
				// 			c.mesh.colors[i][2] * c.mask.B,
				// 			c.mesh.colors[i][3] * c.mask.A,
				// 		}
				// 	}

				// 	texBuf := *(destBuffs[2]).(*[]Vec2)
				// 	for i := range c.mesh.texCoords {
				// 		texBuf[i] = c.mesh.texCoords[i]
				// 	}
				//================================================================================

			}
		}
	}

	r.buffer.Draw()
}

func (r *RenderPass) SetTexture(slot int, texture *Texture) {
	// TODO - use correct texture slot
	r.texture = texture
}

func (r *RenderPass) SetUniform(name string, value interface{}) {
	r.uniforms[name] = value
}

func (r *RenderPass) Add(mesh *Mesh, mat Mat4, mask RGBA, material Material) {
	r.dirty = true
	r.commands[r.currentLayer] = append(r.commands[r.currentLayer], drawCommand{
		0, mesh, mat, mask, material,
	})
}
