use wasm_bindgen::prelude::*;
use web_sys::{WebGl2RenderingContext, WebGlProgram, WebGlBuffer, WebGlTexture};

#[wasm_bindgen]
pub struct TextRenderer {
    program: WebGlProgram,
    vbo: Option<WebGlBuffer>,
    texture: Option<WebGlTexture>,
}

impl TextRenderer {
    pub fn new(gl: &WebGl2RenderingContext, vert_source: &str, frag_source: &str) -> Result<Self, JsValue> {
        let program = crate::compile_and_link(gl, vert_source, frag_source)
            .map_err(|e| JsValue::from_str(&e))?;
            
        let vbo = gl.create_buffer();
        let texture = gl.create_texture();

        // Bind and setup mock 2x2 red texture representing the font atlas
        gl.bind_texture(WebGl2RenderingContext::TEXTURE_2D, texture.as_ref());
        let mock_pixels: [u8; 12] = [
            255, 0, 0, 255, 0, 0,
            255, 0, 0, 255, 0, 0
        ];
        gl.tex_image_2d_with_i32_and_i32_and_i32_and_format_and_type_and_opt_u8_array(
            WebGl2RenderingContext::TEXTURE_2D,
            0,
            WebGl2RenderingContext::RGB as i32,
            2,
            2,
            0,
            WebGl2RenderingContext::RGB,
            WebGl2RenderingContext::UNSIGNED_BYTE,
            Some(&mock_pixels)
        ).map_err(|e| JsValue::from_str(&format!("{:?}", e)))?;

        gl.tex_parameter_i32(WebGl2RenderingContext::TEXTURE_2D, WebGl2RenderingContext::TEXTURE_MIN_FILTER, WebGl2RenderingContext::LINEAR as i32);

        Ok(TextRenderer { program, vbo, texture })
    }

    pub fn render_text(&self, gl: &WebGl2RenderingContext, _text: &str, _x: f32, _y: f32) {
        gl.use_program(Some(&self.program));
        gl.active_texture(WebGl2RenderingContext::TEXTURE0);
        gl.bind_texture(WebGl2RenderingContext::TEXTURE_2D, self.texture.as_ref());

        if let Some(ref buffer) = self.vbo {
            gl.bind_buffer(WebGl2RenderingContext::ARRAY_BUFFER, Some(buffer));
            
            // Quad vertices mapping the font atlas coordinates
            let vertices: [f32; 16] = [
                -0.95, 0.95, 0.0, 0.0,
                -0.85, 0.95, 1.0, 0.0,
                -0.95, 0.85, 0.0, 1.0,
                -0.85, 0.85, 1.0, 1.0
            ];

            unsafe {
                let view = js_sys::Float32Array::view(&vertices);
                gl.buffer_data_with_array_buffer_view(
                    WebGl2RenderingContext::ARRAY_BUFFER,
                    &view,
                    WebGl2RenderingContext::STATIC_DRAW
                );
            }

            gl.draw_arrays(WebGl2RenderingContext::TRIANGLE_STRIP, 0, 4);
        }
    }
}
