#version 300 es
precision mediump float;

in vec2 vTexCoord;
out vec4 fragColor;

uniform sampler2D msdf_texture;
uniform vec4 text_color;

float median(float r, float g, float b) {
    return max(min(r, g), min(max(r, g), b));
}

void main() {
    vec3 sample_val = texture(msdf_texture, vTexCoord).rgb;
    float sig_dist = median(sample_val.r, sample_val.g, sample_val.b) - 0.5;
    float opacity = clamp(sig_dist / fwidth(sig_dist) + 0.5, 0.0, 1.0);
    fragColor = vec4(text_color.rgb, text_color.a * opacity);
}
