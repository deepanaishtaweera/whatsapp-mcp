import os
import sqlite3
import tempfile
import unittest
from unittest.mock import Mock, patch
import whatsapp

ALEX = "123@s.whatsapp.net"
GROUP = "120363@g.us"
QUIET = "456@s.whatsapp.net"

class UnreadTests(unittest.TestCase):
    def setUp(self):
        fd, self.db_path = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        conn = sqlite3.connect(self.db_path)
        conn.executescript("""
            CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP,
                last_read_time TIMESTAMP, marked_unread BOOLEAN NOT NULL DEFAULT 0);
            CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP,
                is_from_me BOOLEAN, is_deleted BOOLEAN NOT NULL DEFAULT 0, PRIMARY KEY (id, chat_jid));
        """)
        # Alex was read up to 10:00 UTC; the times use different UTC offsets
        conn.execute("INSERT INTO chats VALUES (?, 'Alex', '2026-10-30 12:30:00+02:00', '2026-10-30 10:00:00+00:00', 0)", (ALEX,))
        # The group was never read
        conn.execute("INSERT INTO chats VALUES (?, 'Lunch club', '2026-10-30 09:00:00+00:00', NULL, 0)", (GROUP,))
        # Nothing new, but marked unread by hand
        conn.execute("INSERT INTO chats VALUES (?, 'Sam', '2026-10-30 08:00:00+00:00', '2026-10-30 08:00:00+00:00', 1)", (QUIET,))
        messages = [
            ("a1", ALEX, "123", "old", "2026-10-30 11:30:00+02:00", 0, 0),   # 09:30 UTC, read
            ("a2", ALEX, "123", "new", "2026-10-30 12:15:00+02:00", 0, 0),   # 10:15 UTC, unread
            ("a3", ALEX, "111", "mine", "2026-10-30 12:20:00+02:00", 1, 0),  # ours, never unread
            ("a4", ALEX, "123", "gone", "2026-10-30 12:30:00+02:00", 0, 1),  # deleted
            ("g1", GROUP, "789", "hi", "2026-10-30 09:00:00+00:00", 0, 0),
            ("g2", GROUP, "999", "hey", "2026-10-30 09:00:00+00:00", 0, 0),
        ]
        conn.executemany("INSERT INTO messages VALUES (?, ?, ?, ?, ?, ?, ?)", messages)
        conn.commit()
        conn.close()
        patcher = patch.object(whatsapp, "MESSAGES_DB_PATH", self.db_path)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(os.remove, self.db_path)

    def test_unread_counts(self):
        chats = {c.jid: c for c in whatsapp.list_chats()}
        self.assertEqual(chats[ALEX].unread_count, 1)
        self.assertEqual(chats[GROUP].unread_count, 2)
        self.assertEqual(chats[QUIET].unread_count, 0)
        self.assertTrue(chats[QUIET].marked_unread)
        self.assertFalse(chats[ALEX].marked_unread)

    def test_unread_only(self):
        chats = whatsapp.list_chats(unread_only=True)
        self.assertEqual({c.jid for c in chats}, {ALEX, GROUP, QUIET})
        conn = sqlite3.connect(self.db_path)
        conn.execute("UPDATE chats SET marked_unread = 0 WHERE jid = ?", (QUIET,))
        conn.execute("UPDATE chats SET last_read_time = '2026-10-30 12:00:00+00:00' WHERE jid = ?", (ALEX,))
        conn.commit()
        conn.close()
        self.assertEqual([c.jid for c in whatsapp.list_chats(unread_only=True)], [GROUP])

    def test_chat_lookups_include_unread(self):
        self.assertEqual(whatsapp.get_chat(ALEX).unread_count, 1)
        without_last = whatsapp.get_chat(GROUP, include_last_message=False)
        self.assertEqual(without_last.unread_count, 2)
        self.assertIsNone(without_last.last_message)
        self.assertEqual(whatsapp.list_chats(include_last_message=False)[0].unread_count, 1)
        self.assertEqual(whatsapp.get_direct_chat_by_contact("123").unread_count, 1)
        self.assertEqual(whatsapp.get_contact_chats(ALEX)[0].unread_count, 1)

    def test_mark_as_read(self):
        response = Mock(status_code=200)
        response.json.return_value = {"success": True, "message": "Marked 1 messages as read"}
        with patch.object(whatsapp.requests, "post", return_value=response) as post:
            result = whatsapp.mark_as_read(ALEX)
        self.assertTrue(post.call_args.args[0].endswith("/mark-read"))
        self.assertEqual(post.call_args.kwargs["json"], {"chat_jid": ALEX})
        self.assertTrue(result["success"])

if __name__ == "__main__":
    unittest.main()
