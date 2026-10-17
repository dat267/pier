// a comment
#version 330 core

layout(location = 0) in vec3 position;
uniform mat4 modelViewProjection;

out vec4 fragColor;

void main() {
    gl_Position = modelViewProjection * vec4(position, 1.0);
    fragColor = vec4(1.0, 0.5, 0.2, 1.0);
}
