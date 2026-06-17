use wasm_bindgen::prelude::*;
use web_sys::{HtmlCanvasElement, WebGl2RenderingContext, WebGlProgram, WebGlShader};

pub mod chart;
pub mod orderbook;
pub mod text;

#[wasm_bindgen]
extern "C" {
    #[wasm_bindgen(js_namespace = console)]
    fn log(s: &str);
}

macro_rules! console_log {
    ($($t:tt)*) => (log(&format_args!($($t)*).to_string()))
}

#[wasm_bindgen]
pub struct RendererContext {
    gl: WebGl2RenderingContext,
    chart_renderer: chart::ChartRenderer,
    orderbook_visualizer: orderbook::OrderBookVisualizer,
    text_renderer: text::TextRenderer,
}

#[wasm_bindgen]
impl RendererContext {
    #[wasm_bindgen(constructor)]
    pub fn new(canvas_id: &str) -> Result<RendererContext, JsValue> {
        console_error_panic_hook::set_once();
        
        let document = web_sys::window().unwrap().document().unwrap();
        let canvas = document.get_element_by_id(canvas_id).unwrap();
        let canvas: HtmlCanvasElement = canvas.dyn_into::<HtmlCanvasElement>()?;

        let gl = canvas
            .get_context("webgl2")?
            .unwrap()
            .dyn_into::<WebGl2RenderingContext>()?;

        // Load shader programs
        let chart_vert = include_str!("shaders/chart.vert");
        let chart_frag = include_str!("shaders/chart.frag");
        let orderbook_comp = include_str!("shaders/orderbook.comp");
        let text_vert = include_str!("shaders/text.vert");
        let text_frag = include_str!("shaders/text.frag");

        let chart_renderer = chart::ChartRenderer::new(&gl, chart_vert, chart_frag)?;
        let orderbook_visualizer = orderbook::OrderBookVisualizer::new(&gl, chart_vert, orderbook_comp)?;
        let text_renderer = text::TextRenderer::new(&gl, text_vert, text_frag)?;

        console_log!("Quantum Terminal v4.0 Renderer initialized with custom shaders.");

        Ok(RendererContext { 
            gl, 
            chart_renderer,
            orderbook_visualizer,
            text_renderer
        })
    }

    #[wasm_bindgen]
    pub fn render_frame(&self) {
        self.gl.clear_color(0.02, 0.02, 0.04, 1.0); // Void blue base color
        self.gl.clear(WebGl2RenderingContext::COLOR_BUFFER_BIT | WebGl2RenderingContext::DEPTH_BUFFER_BIT);

        // Render individual components
        self.chart_renderer.render(&self.gl);
        self.orderbook_visualizer.render(&self.gl);
        self.text_renderer.render_text(&self.gl, "LIVE HFT", 0.0, 0.9);
    }
}

pub fn compile_shader(
    gl: &WebGl2RenderingContext,
    shader_type: u32,
    source: &str,
) -> Result<WebGlShader, String> {
    let shader = gl
        .create_shader(shader_type)
        .ok_or_else(|| String::from("Unable to create shader object"))?;
    gl.shader_source(&shader, source);
    gl.compile_shader(&shader);

    if gl
        .get_shader_parameter(&shader, WebGl2RenderingContext::COMPILE_STATUS)
        .as_bool()
        .unwrap_or(false)
    {
        Ok(shader)
    } else {
        Err(gl
            .get_shader_info_log(&shader)
            .unwrap_or_else(|| String::from("Unknown error creating shader")))
    }
}

pub fn link_program(
    gl: &WebGl2RenderingContext,
    vert_shader: &WebGlShader,
    frag_shader: &WebGlShader,
) -> Result<WebGlProgram, String> {
    let program = gl
        .create_program()
        .ok_or_else(|| String::from("Unable to create program object"))?;

    gl.attach_shader(&program, vert_shader);
    gl.attach_shader(&program, frag_shader);
    gl.link_program(&program);

    if gl
        .get_program_parameter(&program, WebGl2RenderingContext::LINK_STATUS)
        .as_bool()
        .unwrap_or(false)
    {
        Ok(program)
    } else {
        Err(gl
            .get_program_info_log(&program)
            .unwrap_or_else(|| String::from("Unknown error creating program object")))
    }
}

pub fn compile_and_link(
    gl: &WebGl2RenderingContext,
    vert_source: &str,
    frag_source: &str,
) -> Result<WebGlProgram, String> {
    let vert_shader = compile_shader(gl, WebGl2RenderingContext::VERTEX_SHADER, vert_source)?;
    let frag_shader = compile_shader(gl, WebGl2RenderingContext::FRAGMENT_SHADER, frag_source)?;
    link_program(gl, &vert_shader, &frag_shader)
}
