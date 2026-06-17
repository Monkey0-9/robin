use wasm_bindgen::prelude::*;
use web_sys::{WebGl2RenderingContext, WebGlProgram, WebGlBuffer};

#[wasm_bindgen]
pub struct OrderBookVisualizer {
    program: WebGlProgram,
    vbo: Option<WebGlBuffer>,
}

impl OrderBookVisualizer {
    pub fn new(gl: &WebGl2RenderingContext, vert_source: &str, frag_source: &str) -> Result<Self, JsValue> {
        let program = crate::compile_and_link(gl, vert_source, frag_source)
            .map_err(|e| JsValue::from_str(&e))?;
            
        let vbo = gl.create_buffer();

        Ok(OrderBookVisualizer { program, vbo })
    }

    pub fn render(&self, gl: &WebGl2RenderingContext) {
        gl.use_program(Some(&self.program));

        if let Some(ref buffer) = self.vbo {
            gl.bind_buffer(WebGl2RenderingContext::ARRAY_BUFFER, Some(buffer));

            // Setup mock bids/asks depth bars: [offset_x, offset_y, size_x, size_y]
            let depth_data: [f32; 16] = [
                -0.9, -0.8, 0.4, 0.05,
                -0.9, -0.7, 0.6, 0.05,
                -0.9, -0.6, 0.2, 0.05,
                -0.9, -0.5, 0.8, 0.05
            ];

            unsafe {
                let view = js_sys::Float32Array::view(&depth_data);
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
