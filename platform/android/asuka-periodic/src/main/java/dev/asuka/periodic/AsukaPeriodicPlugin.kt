package dev.asuka.periodic

import android.content.Context
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import dev.asuka.AsukaPeriodicHub
import dev.asuka.AsukaPlugin
import dev.asuka.AsukaRuntime
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** asuka/sdk/periodic's scheduling: a unique periodic work request for each task. */
class AsukaPeriodicPlugin : AsukaPlugin {
    override fun register(rt: AsukaRuntime) {
        AsukaPeriodicHub.schedule = { r, name, seconds, network, charging ->
            val c = Constraints.Builder()
                .setRequiredNetworkType(if (network) NetworkType.CONNECTED else NetworkType.NOT_REQUIRED)
                .setRequiresCharging(charging)
                .build()
            val req = PeriodicWorkRequestBuilder<AsukaPeriodicWorker>(maxOf(seconds, 15 * 60), TimeUnit.SECONDS)
                .setConstraints(c)
                .setInputData(workDataOf("name" to name))
                .build()
            WorkManager.getInstance(r.app).enqueueUniquePeriodicWork("asuka.$name", ExistingPeriodicWorkPolicy.UPDATE, req)
        }
        AsukaPeriodicHub.cancel = { r, name -> WorkManager.getInstance(r.app).cancelUniqueWork("asuka.$name") }
    }
}

/** Runs a task: the core starts if it hasn't, and the Go code reports it done (in at most 9 minutes; WorkManager's limit is 10). */
class AsukaPeriodicWorker(context: Context, params: WorkerParameters) : Worker(context, params) {
    override fun doWork(): Result {
        val name = inputData.getString("name") ?: return Result.success()
        val done = CountDownLatch(1)
        var ok = false
        AsukaPeriodicHub.run(AsukaRuntime.get(applicationContext), name, 9 * 60) { ok = it; done.countDown() }
        if (!done.await(9 * 60L + 10, TimeUnit.SECONDS)) {
            AsukaPeriodicHub.stopped(name)
            return Result.retry()
        }
        return if (ok) Result.success() else Result.retry()
    }

    override fun onStopped() {
        AsukaPeriodicHub.stopped(inputData.getString("name") ?: return)
    }
}
