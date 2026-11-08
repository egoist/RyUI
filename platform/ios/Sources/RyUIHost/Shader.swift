#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif
#if RYUI_SHADER
import Metal

/// A Shader: a CAMetalLayer a fragment program draws, its source the app's
/// shade(uv) inside a struct whose members are time, size and the
/// uniforms (so the source reads them as globals, as AGSL's does), with a
/// full-box triangle around it. Programs are compiled once per source and
/// set of uniform names. Animated, it draws at each frame of a display
/// link; else when its size or uniforms change.
final class RyUIShaderView: UIView {
    override class var layerClass: AnyClass { CAMetalLayer.self }
    private var metal: CAMetalLayer { layer as! CAMetalLayer }

    static let device = MTLCreateSystemDefaultDevice()
    static let queue = device?.makeCommandQueue()
    static var pipelines: [String: MTLRenderPipelineState] = [:]

    var source = "" { didSet { pipeline = nil; setNeedsLayout() } }
    var uniforms: [(String, SIMD4<Float>)] = [] {
        didSet {
            if uniforms.map(\.0) != oldValue.map(\.0) { pipeline = nil }
            draw()
        }
    }
    var animated = false { didSet { updateLink() } }
    private var pipeline: MTLRenderPipelineState?
    private var link: CADisplayLink?
    private var start = CACurrentMediaTime()

    override init(frame: CGRect) {
        super.init(frame: frame)
        metal.device = Self.device
        metal.pixelFormat = .bgra8Unorm
        metal.isOpaque = false
        metal.framebufferOnly = true
        isAccessibilityElement = false
    }
    required init?(coder: NSCoder) { fatalError() }

    override func didMoveToWindow() {
        super.didMoveToWindow()
        start = CACurrentMediaTime()
        updateLink()
        draw()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let s = window?.screen.scale ?? traitCollection.displayScale
        metal.contentsScale = s
        metal.drawableSize = CGSize(width: bounds.width * s, height: bounds.height * s)
        draw()
    }

    private func updateLink() {
        let on = animated && window != nil
        if on, link == nil {
            let l = CADisplayLink(target: self, selector: #selector(tick))
            l.add(to: .main, forMode: .common)
            link = l
        } else if !on, let l = link {
            l.invalidate()
            link = nil
        }
    }

    @objc private func tick() { draw() }

    /// The Metal source around the app's function.
    static func program(_ source: String, _ names: [String]) -> String {
        let members = names.map { "    float4 \($0);" }.joined(separator: "\n")
        let loads = names.enumerated().map { i, n in "    s.\(n) = float4(u[\(4 + 4 * i)], u[\(5 + 4 * i)], u[\(6 + 4 * i)], u[\(7 + 4 * i)]);" }.joined(separator: "\n")
        return """
        #include <metal_stdlib>
        using namespace metal;
        // AGSL's and GLSL's names, for a source both run.
        float mod(float x, float y) { return x - y * floor(x / y); }
        float2 mod(float2 x, float2 y) { return x - y * floor(x / y); }
        float3 mod(float3 x, float3 y) { return x - y * floor(x / y); }
        float4 mod(float4 x, float4 y) { return x - y * floor(x / y); }
        float2 mod(float2 x, float y) { return x - y * floor(x / y); }
        float3 mod(float3 x, float y) { return x - y * floor(x / y); }
        float4 mod(float4 x, float y) { return x - y * floor(x / y); }
        float atan(float y, float x) { return atan2(y, x); }
        struct RyUIShader {
            float time;
            float2 size;
        \(members)
        \(source)
        };
        struct RyUIVertex { float4 position [[position]]; float2 uv; };
        vertex RyUIVertex ryui_vertex(uint id [[vertex_id]]) {
            float2 p = float2((id << 1) & 2, id & 2);
            RyUIVertex v;
            v.position = float4(p * float2(2.0, -2.0) + float2(-1.0, 1.0), 0.0, 1.0);
            v.uv = p;
            return v;
        }
        fragment float4 ryui_fragment(RyUIVertex in [[stage_in]], constant float *u [[buffer(0)]]) {
            RyUIShader s;
            s.time = u[0];
            s.size = float2(u[1], u[2]);
        \(loads)
            float4 c = s.shade(in.uv);
            return float4(c.rgb * c.a, c.a);
        }
        """
    }

    private func makePipeline() -> MTLRenderPipelineState? {
        guard let device = Self.device, !source.isEmpty else { return nil }
        let names = uniforms.map(\.0)
        let key = source + "\u{0}" + names.joined(separator: ",")
        if let p = Self.pipelines[key] { return p }
        do {
            let lib = try device.makeLibrary(source: Self.program(source, names), options: nil)
            let d = MTLRenderPipelineDescriptor()
            d.vertexFunction = lib.makeFunction(name: "ryui_vertex")
            d.fragmentFunction = lib.makeFunction(name: "ryui_fragment")
            d.colorAttachments[0].pixelFormat = .bgra8Unorm
            let p = try device.makeRenderPipelineState(descriptor: d)
            Self.pipelines[key] = p
            return p
        } catch {
            NSLog("ryui: ui.Shader's source doesn't compile with Metal: %@", "\(error)")
            Self.pipelines[key] = nil
            return nil
        }
    }

    func draw() {
        guard window != nil, bounds.width > 0, bounds.height > 0 else { return }
        if pipeline == nil { pipeline = makePipeline() }
        guard let pipeline, let queue = Self.queue, let drawable = metal.nextDrawable(),
              let buffer = queue.makeCommandBuffer() else { return }
        let pass = MTLRenderPassDescriptor()
        pass.colorAttachments[0].texture = drawable.texture
        pass.colorAttachments[0].loadAction = .clear
        pass.colorAttachments[0].clearColor = MTLClearColor(red: 0, green: 0, blue: 0, alpha: 0)
        pass.colorAttachments[0].storeAction = .store
        guard let enc = buffer.makeRenderCommandEncoder(descriptor: pass) else { return }
        var u: [Float] = [Float(CACurrentMediaTime() - start), Float(bounds.width), Float(bounds.height), 0]
        for (_, v) in uniforms { u += [v.x, v.y, v.z, v.w] }
        enc.setRenderPipelineState(pipeline)
        u.withUnsafeBytes { enc.setFragmentBytes($0.baseAddress!, length: $0.count, index: 0) }
        enc.drawPrimitives(type: .triangle, vertexStart: 0, vertexCount: 3)
        enc.endEncoding()
        buffer.present(drawable)
        buffer.commit()
    }
}
#endif

@MainActor extension RyUIRuntime {
    func makeShader() -> UIView {
        #if RYUI_SHADER
        return RyUIShaderView(frame: .zero)
        #else
        return RyUIContainerView()
        #endif
    }

    func setShaderProp(_ n: RyUINode, _ prop: UInt16, _ v: RyUIValue) {
        #if RYUI_SHADER
        guard let s = n.view as? RyUIShaderView else { return }
        switch prop {
        case RyUIProp.Shader.source:
            s.source = v.string
        case RyUIProp.Shader.animated:
            s.animated = v.bool
        case RyUIProp.Shader.uniforms:
            guard case .bytes(let b) = v, b.count >= 4 else { return }
            var r = RyUIReader(b)
            var us: [(String, SIMD4<Float>)] = []
            for _ in 0..<Int(r.u32()) {
                let name = r.string()
                us.append((name, SIMD4(r.f32(), r.f32(), r.f32(), r.f32())))
            }
            s.uniforms = us
        default:
            break
        }
        #endif
    }
}
#endif
