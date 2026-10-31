// The Compose side of the benchmarks (bench/README.md): the same
// scenarios, frame by frame, as bench/ryui and bench/swiftui, written as a
// Compose app would be (state per row; the shared tick read through a
// lambda, so only the rows recompose). The meter is bench/ryui's Meter.kt.
package dev.ryui.bench.compose

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableIntState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.dp
import dev.ryui.app.BenchMeter

const val ROWS = 1000 // the mount and update scenarios' rows
const val LIST = 10000 // the scroll scenario's

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        BenchMeter.install(this)
        setContent { MaterialTheme { Surface(Modifier.fillMaxSize()) { Bench() } } }
    }
}

@Composable
fun Bench() {
    var phase by remember { mutableIntStateOf(0) } // 0 ready, 1 the rows, 2 the list
    val values = remember { List(ROWS) { mutableIntStateOf(0) } }
    var tick by remember { mutableIntStateOf(0) } // every row shows it: update-all changes them all
    val list = rememberLazyListState()
    val step = with(LocalDensity.current) { 8.dp.toPx() }
    // The schedule, in frames; the same in each app.
    LaunchedEffect(Unit) {
        var frame = 0
        var done = false
        while (!done) withFrameNanos {
            frame++
            val f = frame
            val m = BenchMeter
            when {
                f == 2 -> { m.startup(); m.memory("memory-idle") }
                f == 29 -> m.begin("mount") // a frame ahead: what's measured starts with the next
                f == 30 -> phase = 1
                f == 36 -> { m.end(); m.memory("memory-mount") }
                f == 60 -> m.begin("update-one")
                f in 61..300 -> values[(f - 61) % ROWS].intValue++
                f == 301 -> m.end()
                f == 330 -> m.begin("update-all")
                f in 331..450 -> tick++
                f == 451 -> m.end()
                f == 480 -> phase = 2
                f == 500 -> m.begin("scroll")
                f in 501..800 -> list.dispatchRawDelta(step)
                f == 801 -> { m.end(); m.memory("memory-list") }
                f == 830 -> { m.done(); done = true }
            }
        }
    }
    when (phase) {
        1 -> Column(Modifier.fillMaxSize().safeDrawingPadding().verticalScroll(rememberScrollState())) {
            for (i in 0 until ROWS) RowItem(i, values[i]) { tick }
        }
        2 -> LazyColumn(Modifier.fillMaxSize().safeDrawingPadding(), state = list) {
            items(LIST, key = { it }) { i ->
                Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Text("Row $i", style = MaterialTheme.typography.titleMedium)
                    Text("Detail ${i * 7 % 1000}", color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        else -> Text("Ready", Modifier.safeDrawingPadding(), style = MaterialTheme.typography.displaySmall)
    }
}

@Composable
fun RowItem(index: Int, value: MutableIntState, tick: () -> Int) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Text("Row $index", Modifier.weight(1f))
        Text("${value.intValue + tick()}")
    }
}
