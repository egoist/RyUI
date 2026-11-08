#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif
#if RYUI_SHADER
import Metal

/// A compiled ui.Shader (package shader), as the core sends it: its Metal
/// library, and where each uniform lane goes in its buffer.
struct RyUIShaderCode {
    var metal = ""
    var lanes = 0, size = 0, scale = 0, time = -1
    var metalSize = 0
    var offsets: [Int] = [], bools: [Bool] = []

    init?(_ b: UnsafeRawBufferPointer) {
        guard b.count >= 4 else { return nil }
        var r = RyUIReader(b)
        _ = r.string() // the Kage source: the CPU's
        metal = r.string()
        _ = r.string() // AGSL: Android's
        lanes = Int(r.u32()); size = Int(r.u32()); scale = Int(r.u32()); time = Int(r.u32()) - 1
        metalSize = Int(r.u32())
        for _ in 0..<lanes {
            let o = r.u32()
            offsets.append(Int(o & 0x7fff_ffff))
            bools.append(o & 0x8000_0000 != 0)
        }
    }
}

/// A Shader: a CAMetalLayer the shader's Metal library draws, a triangle
/// over the whole box (ryui_vertex, ryui_fragment), its uniforms in a
/// buffer as the code lays them out. The host sets the size, the scale
/// and Time. Pipelines are made once per library. Animated, it draws at
/// each frame of a display link; else when its size or uniforms change.
final class RyUIShaderView: UIView {
    override class var layerClass: AnyClass { CAMetalLayer.self }
    private var metal: CAMetalLayer { layer as! CAMetalLayer }

    static let device = MTLCreateSystemDefaultDevice()
    static let queue = device?.makeCommandQueue()
    static var pipelines: [String: MTLRenderPipelineState] = [:]
    static var failed: Set<String> = []
    /// What the library's image parameters take: ui.Shader reads no images.
    static let blank: MTLTexture? = {
        let d = MTLTextureDescriptor.texture2DDescriptor(pixelFormat: .rgba8Unorm, width: 1, height: 1, mipmapped: false)
        return device?.makeTexture(descriptor: d)
    }()

    var code: RyUIShaderCode? {
        didSet {
            pipeline = nil
            if words.count != code?.lanes ?? 0 { words = Array(repeating: 0, count: code?.lanes ?? 0) }
            draw()
        }
    }
    var words: [UInt32] = []
    var animated = false { didSet { updateLink() } }
    private var pipeline: MTLRenderPipelineState?
    private var link: CADisplayLink?
    private var start = CACurrentMediaTime()
    private var buffer: [UInt8] = []

    override init(frame: CGRect) {
        super.init(frame: frame)
        metal.device = Self.device
        metal.pixelFormat = .bgra8Unorm
        metal.isOpaque = false
        metal.framebufferOnly = true
        isAccessibilityElement = false
    }
    required init?(coder: NSCoder) { fatalError() }

    func setWords(_ b: UnsafeRawBufferPointer) {
        var r = RyUIReader(b)
        words = (0..<(b.count / 4)).map { _ in r.u32() }
        draw()
    }

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

    private func makePipeline(_ source: String) -> MTLRenderPipelineState? {
        if let p = Self.pipelines[source] { return p }
        guard let device = Self.device, !Self.failed.contains(source) else { return nil }
        do {
            let lib = try device.makeLibrary(source: source, options: nil)
            let d = MTLRenderPipelineDescriptor()
            d.vertexFunction = lib.makeFunction(name: "ryui_vertex")
            d.fragmentFunction = lib.makeFunction(name: "ryui_fragment")
            d.colorAttachments[0].pixelFormat = .bgra8Unorm
            let p = try device.makeRenderPipelineState(descriptor: d)
            Self.pipelines[source] = p
            return p
        } catch {
            NSLog("ryui: a ui.Shader's Metal library doesn't compile: %@", "\(error)")
            Self.failed.insert(source)
            return nil
        }
    }

    func draw() {
        guard let code, window != nil, bounds.width > 0, bounds.height > 0 else { return }
        if pipeline == nil { pipeline = makePipeline(code.metal) }
        guard let pipeline, let queue = Self.queue, let drawable = metal.nextDrawable(),
              let cmd = queue.makeCommandBuffer() else { return }
        if words.count < code.lanes { words += Array(repeating: 0, count: code.lanes - words.count) }
        let scale = Float(metal.contentsScale)
        words[code.size] = Float(bounds.width).bitPattern
        words[code.size + 1] = Float(bounds.height).bitPattern
        words[code.scale] = scale.bitPattern
        if code.time >= 0 { words[code.time] = Float(CACurrentMediaTime() - start).bitPattern }
        if buffer.count != max(code.metalSize, 16) { buffer = Array(repeating: 0, count: max(code.metalSize, 16)) }
        for i in 0..<code.lanes {
            let o = code.offsets[i]
            if code.bools[i] {
                buffer[o] = words[i] != 0 ? 1 : 0
            } else {
                buffer.withUnsafeMutableBytes { $0.storeBytes(of: words[i].littleEndian, toByteOffset: o, as: UInt32.self) }
            }
        }
        let pass = MTLRenderPassDescriptor()
        pass.colorAttachments[0].texture = drawable.texture
        pass.colorAttachments[0].loadAction = .clear
        pass.colorAttachments[0].clearColor = MTLClearColor(red: 0, green: 0, blue: 0, alpha: 0)
        pass.colorAttachments[0].storeAction = .store
        guard let enc = cmd.makeRenderCommandEncoder(descriptor: pass) else { return }
        enc.setRenderPipelineState(pipeline)
        buffer.withUnsafeBytes { enc.setFragmentBytes($0.baseAddress!, length: $0.count, index: 0) }
        for i in 0..<4 { enc.setFragmentTexture(Self.blank, index: i) }
        enc.drawPrimitives(type: .triangle, vertexStart: 0, vertexCount: 3)
        enc.endEncoding()
        cmd.present(drawable)
        cmd.commit()
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
        case RyUIProp.Shader.code:
            if case .bytes(let b) = v { s.code = RyUIShaderCode(b) }
        case RyUIProp.Shader.uniforms:
            if case .bytes(let b) = v { s.setWords(b) }
        case RyUIProp.Shader.animated:
            s.animated = v.bool
        default:
            break
        }
        #endif
    }
}
#endif
