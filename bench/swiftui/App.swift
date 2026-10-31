// The SwiftUI side of the benchmarks (bench/README.md): the same
// scenarios, frame by frame, as bench/ryui and bench/compose, written as
// a SwiftUI app would be (Observation, a row's value observed by its row
// alone). The meter is bench/ryui/ios/Meter.swift, compiled in.
import SwiftUI
import Observation
import UIKit

let rowCount = 1000 // the mount and update scenarios' rows
let listCount = 10000 // the scroll scenario's

@Observable final class RowModel {
    var value = 0
}

@Observable final class Model {
    var phase = 0 // 0 ready, 1 the rows, 2 the list
    var tick = 0 // every row shows it: update-all changes them all
    let rows = (0..<rowCount).map { _ in RowModel() }
}

/// Runs the schedule, in frames: the same in each app.
final class Driver: NSObject {
    let model: Model
    var frame = 0
    var link: CADisplayLink?

    init(_ m: Model) { model = m }

    func start() {
        guard link == nil else { return }
        let l = CADisplayLink(target: self, selector: #selector(step))
        l.add(to: .main, forMode: .common)
        link = l
    }

    @objc func step() {
        frame += 1
        let f = frame, m = BenchMeter.shared
        switch f {
        case 2: m.startup(); m.memory("memory-idle")
        case 29: m.begin("mount") // a frame ahead: what's measured starts with the next
        case 30: model.phase = 1
        case 36: m.end(); m.memory("memory-mount")
        case 60: m.begin("update-one")
        case 61...300: model.rows[(f - 61) % rowCount].value += 1
        case 301: m.end()
        case 330: m.begin("update-all")
        case 331...450: model.tick += 1
        case 451: m.end()
        case 480: model.phase = 2
        case 500: m.begin("scroll")
        case 501...800:
            // A List is a UICollectionView: scrolled as RyUI's is.
            scrollView()?.contentOffset.y = CGFloat(f - 500) * 8 - (scrollView()?.adjustedContentInset.top ?? 0)
        case 801: m.end(); m.memory("memory-list")
        case 830: m.done()
        default: break
        }
    }

    private weak var found: UIScrollView?
    func scrollView() -> UIScrollView? {
        if let found { return found }
        func find(_ v: UIView) -> UIScrollView? {
            if let s = v as? UICollectionView { return s }
            for c in v.subviews { if let s = find(c) { return s } }
            return nil
        }
        let w = UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow }.first
        found = w.flatMap(find)
        return found
    }
}

struct RowView: View {
    let index: Int
    let row: RowModel
    let model: Model

    var body: some View {
        HStack {
            Text("Row \(index)")
            Spacer()
            Text("\(row.value + model.tick)")
        }
        .padding(.vertical, 8)
        .padding(.horizontal, 16)
    }
}

struct ContentView: View {
    let model: Model

    var body: some View {
        switch model.phase {
        case 1:
            ScrollView {
                VStack(spacing: 0) {
                    ForEach(0..<rowCount, id: \.self) { i in
                        RowView(index: i, row: model.rows[i], model: model)
                    }
                }
            }
        case 2:
            List(0..<listCount, id: \.self) { i in
                VStack(alignment: .leading, spacing: 2) {
                    Text("Row \(i)").font(.headline)
                    Text("Detail \(i * 7 % 1000)").foregroundStyle(.secondary)
                }
            }
            .listStyle(.plain)
        default:
            Text("Ready").font(.largeTitle)
        }
    }
}

@main
struct BenchApp: App {
    let model: Model
    let driver: Driver

    init() {
        BenchMeter.shared.install()
        let m = Model()
        model = m
        driver = Driver(m)
        driver.start()
    }

    var body: some Scene {
        WindowGroup { ContentView(model: model) }
    }
}
