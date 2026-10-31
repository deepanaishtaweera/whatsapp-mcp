import os
import sqlite3
import tempfile
import unittest
from unittest.mock import patch
import whatsapp

CHAT = "123@s.whatsapp.net"

class MessageFormattingTests(unittest.TestCase):
    def setUp(self):
        fd, self.db_path = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        conn = sqlite3.connect(self.db_path)
        # Same schema as the bridge creates
        conn.executescript("""
            CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
            CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP,
                is_from_me BOOLEAN, media_type TEXT, filename TEXT, url TEXT, media_key BLOB, file_sha256 BLOB,
                file_enc_sha256 BLOB, file_length INTEGER, quoted_message_id TEXT, quoted_sender TEXT,
                quoted_content TEXT, edited_at TIMESTAMP, is_deleted BOOLEAN NOT NULL DEFAULT 0,
                PRIMARY KEY (id, chat_jid));
            CREATE TABLE reactions (message_id TEXT, chat_jid TEXT, sender TEXT, emoji TEXT, timestamp TIMESTAMP,
                PRIMARY KEY (message_id, chat_jid, sender));
        """)
        conn.execute("INSERT INTO chats VALUES (?, 'Alex', '2026-10-30 10:05:00+00:00')", (CHAT,))
        rows = [
            ("m1", "lunch?", "2026-10-30 10:00:00+00:00", None, None, None, 0, None),
            ("m2", "yes, see you at 6", "2026-10-30 10:01:00+00:00", "m1", "123", "lunch?", 0, "2026-10-30 10:02:00+00:00"),
            ("m3", "", "2026-10-30 10:03:00+00:00", None, None, None, 1, None),
            ("m4", "the invoice", "2026-10-30 10:04:00+00:00", None, None, None, 0, None),
        ]
        for msg_id, content, ts, quoted_id, quoted_sender, quoted_content, deleted, edited_at in rows:
            conn.execute(
                """INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, media_type,
                   quoted_message_id, quoted_sender, quoted_content, is_deleted, edited_at)
                   VALUES (?, ?, '123', ?, ?, 0, ?, ?, ?, ?, ?, ?)""",
                (msg_id, CHAT, content, ts, "image" if msg_id == "m4" else None,
                 quoted_id, quoted_sender, quoted_content, deleted, edited_at),
            )
        conn.execute("INSERT INTO reactions VALUES ('m1', ?, '456', '👍', '2026-10-30 10:00:30+00:00')", (CHAT,))
        conn.execute("INSERT INTO reactions VALUES ('m1', ?, '789', '❤️', '2026-10-30 10:00:40+00:00')", (CHAT,))
        conn.commit()
        conn.close()

        patcher = patch.object(whatsapp, "MESSAGES_DB_PATH", self.db_path)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(os.remove, self.db_path)

    def lines(self, **kwargs):
        output = whatsapp.list_messages(chat_jid=CHAT, include_context=False, **kwargs)
        return {line.split("From: Alex: ", 1)[1] for line in output.strip().splitlines()}

    def test_reactions_replies_edits_and_deletions(self):
        lines = self.lines()
        self.assertIn("lunch? [reactions: 👍❤️]", lines)
        self.assertIn('[replying to Alex: "lunch?"] yes, see you at 6 (edited)', lines)
        self.assertIn("[deleted]", lines)

    def test_captions_are_searchable(self):
        lines = self.lines(query="invoice")
        self.assertEqual(len(lines), 1)
        self.assertTrue(lines.pop().endswith("] the invoice"))

    def test_message_context_includes_new_fields(self):
        context = whatsapp.get_message_context("m2", before=1, after=1)
        self.assertEqual(context.message.quoted_content, "lunch?")
        self.assertTrue(context.message.is_edited)
        self.assertEqual(context.before[0].reactions, "👍❤️")
        self.assertTrue(context.after[0].is_deleted)

    def test_deleted_media_hides_message_id(self):
        conn = sqlite3.connect(self.db_path)
        conn.execute("UPDATE messages SET is_deleted = 1, content = '' WHERE id = 'm4'")
        conn.commit()
        conn.close()
        self.assertIn("[image] [deleted]", self.lines())

class OldDatabaseTests(unittest.TestCase):
    def test_unmigrated_database_explains_itself(self):
        fd, db_path = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        self.addCleanup(os.remove, db_path)
        conn = sqlite3.connect(db_path)
        conn.executescript("""
            CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
            CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP,
                is_from_me BOOLEAN, media_type TEXT, PRIMARY KEY (id, chat_jid));
        """)
        conn.close()
        with patch.object(whatsapp, "MESSAGES_DB_PATH", db_path):
            self.assertIn("Restart the bridge", whatsapp.list_messages(include_context=False))
            self.assertIn("Restart the bridge", whatsapp.get_last_interaction(CHAT))
            with self.assertRaisesRegex(ValueError, "Restart the bridge"):
                whatsapp.get_message_context("m1")

if __name__ == '__main__':
    unittest.main()
