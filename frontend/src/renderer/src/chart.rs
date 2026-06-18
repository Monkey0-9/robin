use wasm_bindgen::prelude::*;
use web_sys::{WebGl2RenderingContext, WebGlBuffer, WebGlProgram};

#[wasm_bindgen]
pub struct ChartRenderer {
    program: WebGlProgram,
    vbo: Option<WebGlBuffer>,
}

impl ChartRenderer {
    pub fn new(
        gl: &WebGl2RenderingContext,
        vert_source: &str,
        frag_source: &str,
    ) -> Result<Self, JsValue> {
        let program = crate::compile_and_link(gl, vert_source, frag_source)
            .map_err(|e| JsValue::from_str(&e))?;

        let vbo = gl.create_buffer();

        Ok(ChartRenderer { program, vbo })
    }

    pub fn render(&self, gl: &WebGl2RenderingContext) {
        gl.use_program(Some(&self.program));

        if let Some(ref buffer) = self.vbo {
            gl.bind_buffer(WebGl2RenderingContext::ARRAY_BUFFER, Some(buffer));

            // Mock data representation for instanced charts: [x, y, scale_x, scale_y, r, g, b, a]
            let mock_data: [f32; 16] = [
                -0.5, 0.0, 0.1, 0.4, 0.0, 1.0, 0.0, 1.0, // Bid bar (green)
                0.5, 0.0, 0.1, 0.6, 1.0, 0.0, 0.0, 1.0, // Ask bar (red)
            ];

            // Safety check: Bind layout registers
            unsafe {
                let view = js_sys::Float32Array::view(&mock_data);
                gl.buffer_data_with_array_buffer_view(
                    WebGl2RenderingContext::ARRAY_BUFFER,
                    &view,
                    WebGl2RenderingContext::STATIC_DRAW,
                );
            }

            gl.draw_arrays(WebGl2RenderingContext::TRIANGLES, 0, 2);
        }
    }
}
