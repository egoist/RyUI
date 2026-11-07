package dev.ryui

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

// A conversation's notification shows its messages: those the app gives
// (Conversation.Earlier), else those of the notification showing, then
// the new one; a reply is the user's (no sender).
class NotifyTest {
    private val ana = JSONObject().put("id", "ana").put("name", "Ana")
    private val bo = JSONObject().put("id", "bo").put("name", "Bo")

    private fun conversation(earlier: JSONArray? = null) = JSONObject().put("id", "climbing").put("group", true).also { c ->
        earlier?.let { c.put("earlier", it) }
    }

    @Test
    fun earlierGiven() {
        val earlier = JSONArray().put(JSONObject().put("sender", bo).put("text", "Who's in?").put("time", 1000))
            .put(JSONObject().put("text", "Me").put("time", 2000))
        val shown = JSONObject().put("conversation", conversation()).put("sender", ana).put("body", "ignored")
        val ms = RyUIMessages.thread(conversation(earlier), shown, ana, "Saturday?", 3000)
        assertEquals(listOf("Who's in?", "Me", "Saturday?"), ms.map { it.text })
        assertEquals("bo", ms[0].sender?.optString("id"))
        assertNull(ms[1].sender)
        assertEquals(3000L, ms[2].time)
    }

    @Test
    fun addsToShowing() {
        val kept = JSONObject().put("conversation", conversation(JSONArray().put(JSONObject().put("sender", bo).put("text", "Hi").put("time", 1))))
            .put("sender", ana).put("body", "Hello").put("ryui.time", 2L)
        val ms = RyUIMessages.thread(conversation(), kept, bo, "Rope?", 3)
        assertEquals(listOf("Hi", "Hello", "Rope?"), ms.map { it.text })
        assertEquals(listOf(1L, 2L, 3L), ms.map { it.time })
        // Another conversation's notification isn't this one's.
        val other = JSONObject().put("conversation", JSONObject().put("id", "work")).put("sender", ana).put("body", "x")
        assertEquals(listOf("Rope?"), RyUIMessages.thread(conversation(), other, bo, "Rope?", 3).map { it.text })
    }

    @Test
    fun replyIsTheUsers() {
        val kept = JSONObject().put("conversation", conversation()).put("sender", ana).put("body", "Saturday?").put("ryui.time", 5L)
        val all = RyUIMessages.withLatest(kept)
        assertEquals(listOf("Saturday?"), all.map { it.text })
        val me = JSONObject().put("name", "You").put("ryui.me", true)
        val ms = RyUIMessages.thread(conversation(RyUIMessages.toJSON(all)), kept, me, "In!", 6)
        assertEquals(listOf("Saturday?", "In!"), ms.map { it.text })
        assertEquals("ana", ms[0].sender?.optString("id"))
        assertNull(ms[1].sender)
    }

    @Test
    fun shownOfSpec() {
        val spec = JSONObject().put("id", "1").put("title", "Ana").put("body", "hi").put("thread", "climbing")
            .put("data", JSONObject().put("k", "v")).put("ryui.remote", true)
        val s = RyUINotifications.shown(spec, 42)
        assertEquals("climbing", s.getString("thread"))
        assertEquals("v", s.getJSONObject("data").getString("k"))
        assertEquals(42L, s.getLong("time"))
        assertEquals(true, s.getBoolean("remote"))
    }
}
