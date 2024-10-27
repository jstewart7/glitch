package glitch

import (
	"fmt"

	"github.com/unitoftime/glitch/internal/gl"
	"github.com/unitoftime/glitch/internal/mainthread"
	"github.com/unitoftime/glitch/shaders"
)

// Notes: Uniforms are specific to a program: https://stackoverflow.com/questions/10857602/do-uniform-values-remain-in-glsl-shader-if-unbound

type Shader struct {
	program         gl.Program
	uniformLocs     map[string]Uniform
	uniformsMat4    map[string]glMat4 // All uniforms that are glMat4
	uniforms        map[string]any    // All other uniforms
	attrFmt         shaders.VertexFormat
	tmpBuffers      []any
	tmpFloat32Slice []float32
	mainthreadBind  func()

	uniformLoc gl.Uniform

	// TODO: You may be able to do a memory optimization here. where instead of allocating enough for the entire frame to be rendered through this shader, you can make a ringbuffer of VertexBuffers and cycle through those, drawing as you need to. The downside here is that there may be some performance impact if the ringbuffer is too small causing contention between filling the next VertexBuffer and rendering it on the GPU
	pool *BufferPool
}

type Uniform struct {
	name string
	// attrType AttrType
	loc gl.Uniform
}

func NewShader(cfg shaders.ShaderConfig) (*Shader, error) {
	return NewShaderExt(cfg.VertexShader, cfg.FragmentShader, cfg.VertexFormat, cfg.UniformFormat)
}

func NewShaderExt(vertexSource, fragmentSource string, attrFmt shaders.VertexFormat, uniformFmt shaders.UniformFormat) (*Shader, error) {
	shader := &Shader{
		uniformLocs:     make(map[string]Uniform),
		uniformsMat4:    make(map[string]glMat4),
		uniforms:        make(map[string]any),
		attrFmt:         attrFmt,
		tmpFloat32Slice: make([]float32, 0),
	}
	err := mainthread.CallErr(func() error {
		var err error
		shader.program, err = createProgram(vertexSource, fragmentSource)
		if err != nil {
			return err
		}

		for _, uniform := range uniformFmt {
			loc := gl.GetUniformLocation(shader.program, uniform.Name)
			shader.uniformLocs[uniform.Name] = Uniform{uniform.Name, loc}
			// fmt.Println("Found uniform: ", uniform)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	shader.mainthreadBind = func() {
		gl.UseProgram(shader.program)
	}

	// shader.setUniformMat4 = func() {
	// 	gl.UniformMatrix4fv(shader.uniformLoc, shader.tmpFloat32Slice)
	// }

	// Loop through and set all matrices to identity matrices
	// shader.Bind()
	setShader(shader)
	for _, uniform := range uniformFmt {
		// TODO handle other matrices
		if uniform.Type == shaders.AttrMat4 {
			// Setting uniform
			shader.setUniformMat4(uniform.Name, glMat4Ident)
		}
	}

	shader.tmpBuffers = make([]any, len(shader.attrFmt))
	for i, attr := range shader.attrFmt {
		// shader.tmpBuffers[i] = attr.GetBuffer()
		shader.tmpBuffers[i] = getBuffer(attr.Attr)
	}

	defaultBatchSize := 1024 * 8 // 10000 // TODO: arbitrary. make configurable
	shader.pool = NewBufferPool(shader, defaultBatchSize)

	return shader, nil
}

// func (s *Shader) Bind() {
// 	mainthread.Call(s.mainthreadBind)
// }

// func (s *Shader) Bind() {
// 	mainthreadCall(func() {
// 		gl.UseProgram(s.program)
// 	})
// }

func createProgram(vertexSrc, fragmentSrc string) (gl.Program, error) {
	program := gl.CreateProgram()
	if !program.Valid() {
		return gl.Program{}, fmt.Errorf("Could not CreateProgram")
	}

	vertexShader, err := loadShader(gl.VERTEX_SHADER, vertexSrc)
	if err != nil {
		return gl.Program{}, err
	}
	fragmentShader, err := loadShader(gl.FRAGMENT_SHADER, fragmentSrc)
	if err != nil {
		gl.DeleteShader(vertexShader)
		return gl.Program{}, err
	}

	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	gl.LinkProgram(program)

	// Flag shaders for deletion when program is unlinked.
	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	if gl.GetProgrami(program, gl.LINK_STATUS) == 0 {
		defer gl.DeleteProgram(program)
		return gl.Program{}, fmt.Errorf("CreateProgram: %s", gl.GetProgramInfoLog(program))
	}
	return program, nil
}

func loadShader(shaderType gl.Enum, src string) (gl.Shader, error) {
	shader := gl.CreateShader(shaderType)
	if !shader.Valid() {
		return gl.Shader{}, fmt.Errorf("loadShader could not create shader (type %v)", shaderType)
	}
	gl.ShaderSource(shader, src)
	gl.CompileShader(shader)
	if gl.GetShaderi(shader, gl.COMPILE_STATUS) == gl.FALSE {
		defer gl.DeleteShader(shader)
		return gl.Shader{}, fmt.Errorf("loadShader: %s", gl.GetShaderInfoLog(shader))
	}
	return shader, nil
}

// Note: This was me playing around with a way to reduce the amount of memory allocations
var tmpUniformSetter uniformSetter
var tmpUniformSetterMat4 uniformSetterMat4

func init() {
	tmpUniformSetter.FUNC = func() {
		tmpUniformSetter.Func()
	}
	tmpUniformSetterMat4.FUNC = func() {
		tmpUniformSetterMat4.Func()
	}
}

// TODO: Should I use a comparable here? and just force uniforms to be comparable?
func openglEquals(a, b any) bool {
	// Note: https://go.dev/ref/spec#Comparison_operators - For interface equality. They are equal if the types are the same and the comparable value at that location is the same (bubbles down to pointer compare or struct compare)
	return a == b
}

// Binds the shader and sets the uniform
func (s *Shader) SetUniform(name string, value any) bool {
	setShader(s)
	return s.setUniform(name, value)
}

func (s *Shader) setUniform(name string, value any) bool {
	// We need to ensure that all Mat4s go into shader.setUniformMat4()
	switch val := value.(type) {
	case Mat4:
		s.setUniformMat4(name, glm4(val))
	case *Mat4:
		s.setUniformMat4(name, glm4(*val))
	case glMat4:
		s.setUniformMat4(name, val)
	case *glMat4:
		s.setUniformMat4(name, *val)
	}

	currentValue, ok := s.uniforms[name]
	if ok && openglEquals(currentValue, value) {
		return true // Skip because the shader already has the uniform set to this value
	}
	s.uniforms[name] = value

	tmpUniformSetter.shader = s
	tmpUniformSetter.name = name
	tmpUniformSetter.value = value

	mainthread.Call(tmpUniformSetter.FUNC)
	return true // TODO - wrong
}

func (s *Shader) setUniformMat4(name string, value glMat4) bool {
	currentValue, ok := s.uniformsMat4[name]
	if ok && (currentValue == value) {
		return true // Skip because the shader already has the uniform set to this value
	}
	s.uniformsMat4[name] = value

	tmpUniformSetterMat4.shader = s
	tmpUniformSetterMat4.name = name
	tmpUniformSetterMat4.value = value

	mainthread.Call(tmpUniformSetterMat4.FUNC)
	return true // TODO - wrong
}

type uniformSetter struct {
	shader *Shader
	name   string
	value  any
	FUNC   func()
}

func (u *uniformSetter) Func() {
	s := u.shader
	uniformName := u.name
	value := u.value

	uniform, ok := s.uniformLocs[uniformName]
	// TODO - detecting if uniform is invalid, because Valid() checks if it is 0, which is a valid location index
	if !ok /* || !uniform.loc.Valid() */ {
		// TODO - panic or just return false? I feel like its bad if you think you're setting a uniform that doesn't exist.
		panic(fmt.Sprintf("Uniform not found! Or uniform location was invalid: %s", uniformName))
	}

	switch val := value.(type) {
	case float32:
		sliced := []float32{val}
		gl.Uniform1fv(uniform.loc, sliced)
	// gl.Uniform1fv(uniform.loc, val)
	case float64:
		sliced := []float32{float32(val)}
		gl.Uniform1fv(uniform.loc, sliced)

	case glMat4:
		gl.UniformMatrix4fv(uniform.loc, []float32(val[:]))

	case Vec3:
		vec := glv3(val)
		gl.Uniform3fv(uniform.loc, vec[:])
	case Vec4:
		vec := glv4(val)
		gl.Uniform4fv(uniform.loc, vec[:])
	case RGBA: // Same as vec4
		vec := glc4(val)
		gl.Uniform4fv(uniform.loc, vec[:])
	case Mat4:
		s.tmpFloat32Slice = s.tmpFloat32Slice[:0]
		s.tmpFloat32Slice = mat4ToFloat32(val, s.tmpFloat32Slice)
		gl.UniformMatrix4fv(uniform.loc, s.tmpFloat32Slice)
	case *Mat4:
		s.tmpFloat32Slice = s.tmpFloat32Slice[:0]
		s.tmpFloat32Slice = mat4ToFloat32(*val, s.tmpFloat32Slice)
		gl.UniformMatrix4fv(uniform.loc, s.tmpFloat32Slice)
	default:
		panic(fmt.Sprintf("set uniform attr: invalid attribute type: %T", value))
	}
}

func getBuffer(a shaders.Attr) any {
	switch a.Type {
	case shaders.AttrFloat:
		return &[]float32{}
	case shaders.AttrVec2:
		return &[]glVec2{}
	case shaders.AttrVec3:
		return &[]glVec3{}
	case shaders.AttrVec4:
		return &[]glVec4{}
	default:
		panic(fmt.Sprintf("Attr not valid for GetBuffer: %v", a))
	}
}

type uniformSetterMat4 struct {
	shader *Shader
	name   string
	value  glMat4
	FUNC   func()
}

func (u *uniformSetterMat4) Func() {
	uniform, ok := u.shader.uniformLocs[u.name]
	// TODO - detecting if uniform is invalid, because Valid() checks if it is 0, which is a valid location index
	if !ok /* || !uniform.loc.Valid() */ {
		// TODO - panic or just return false? I feel like its bad if you think you're setting a uniform that doesn't exist.
		panic(fmt.Sprintf("Uniform not found! Or uniform location was invalid: %s", u.name))
	}

	gl.UniformMatrix4fv(uniform.loc, []float32(u.value[:]))
}

//--------------------------------------------------------------------------------

func (shader *Shader) BufferMesh(mesh *Mesh, translucent bool) *VertexBuffer {
	// bufferState := BufferState{material, BlendModeNormal} // TODO: Blendmode used to come from renderpass

	if len(mesh.indices)%3 != 0 {
		panic("Cmd.Mesh indices must have 3 indices per triangle!")
	}
	numVerts := len(mesh.positions)
	buffer := NewVertexBuffer(shader, numVerts, len(mesh.indices))
	buffer.deallocAfterBuffer = true

	success := buffer.Reserve(mesh.indices, numVerts, shader.tmpBuffers)
	if !success {
		panic("Something went wrong")
	}

	// cmd := drawCommand{
	// 	mesh, Mat4Ident, White, bufferState,
	// }
	// TODO: Translucent?
	// TODO: Depth sorting?
	batchToBuffers(shader, mesh, glMat4Ident, White)

	// pass.copyToBuffer(cmd, pass.shader.tmpBuffers)

	// TODO: Could use copy funcs if you want to restrict buffer types
	// for bufIdx, attr := range pass.shader.attrFmt {
	// 	switch attr.Swizzle {
	// 	case PositionXYZ:
	// 		buffer.buffers[bufIdx].SetData(mesh.positions)
	// 	case ColorRGBA:
	// 		buffer.buffers[bufIdx].SetData(mesh.colors)
	// 	case TexCoordXY:
	// 		buffer.buffers[bufIdx].SetData(mesh.texCoords)
	// 	default:
	// 		panic("unsupported")
	// 	}
	// }

	return buffer
}

// // This is like batchToBuffer but doesn't pre-apply the model matrix of the mesh
// func (r *RenderPass) copyToBuffer(c drawCommand, destBuffs []interface{}) {
// 	// // For now I'm just going to modify the drawCommand to use Mat4Ident and then pass to batchToBuffers
// 	// c.matrix = Mat4Ident
// 	// batchToBuffers(c, destBuffs)

// 	numVerts := c.filler.NumVerts()
// 	indices := c.filler.Indices()
// 	vertexBuffer := pass.buffer.Reserve(state, indices, numVerts, pass.shader.tmpBuffers)
// 	batchToBuffers(pass.shader, m, mat, mask)
// }
