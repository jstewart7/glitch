#version 300 es

layout (location = 0) in vec2 positionIn;
layout (location = 1) in vec4 colorIn;
layout (location = 2) in vec2 texCoordIn;

out vec4 ourColor;
out vec2 TexCoord;

uniform mat4 projection;
uniform mat4 view;
//uniform mat4 transform;

void main()
{
  gl_Position = projection * view * vec4(positionIn, 0.0, 1.0);
  //	gl_Position = projection * transform * vec4(aPos, 1.0);
  //	gl_Position = vec4(aPos, 1.0);
  ourColor = colorIn;

  TexCoord = vec2(texCoordIn.x, texCoordIn.y);
}
