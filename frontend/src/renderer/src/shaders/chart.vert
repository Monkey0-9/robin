#version 300 es
layout(location = 0) in vec2 position;
layout(location = 1) in vec4 color;
layout(location = 2) in vec2 offset;
layout(location = 3) in vec2 scale;

out vec4 vColor;

void main() {
    vec2 pos = position * scale + offset;
    gl_Position = vec4(pos, 0.0, 1.0);
    vColor = color;
}
