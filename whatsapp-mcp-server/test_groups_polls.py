import os
import sqlite3
import tempfile
import unittest
from unittest.mock import Mock, patch
import whatsapp

CHAT = "120363@g.us"

def bridge_response(status_code, body=None, text=""):
    response = Mock(status_code=status_code, text=text)
    if body is None:
        response.json.side_effect = ValueError("not JSON")
    else:
        response.json.return_value = body
    return response

class GroupTests(unittest.TestCase):
    def post(self, call, body=None, status_code=200):
        response = bridge_response(status_code, body if body is not None else {"success": True, "message": "ok"})
        with patch.object(whatsapp.requests, "post", return_value=response) as post:
            result = call()
        return post, result

    def test_create_group(self):
        body = {"success": True, "message": "Created", "jid": CHAT,
                "participants": [{"jid": "94771234567@s.whatsapp.net", "ok": True, "status": "ok"}]}
        post, result = self.post(lambda: whatsapp.create_group("Family", ["+94771234567"]), body)
        self.assertTrue(post.call_args.args[0].endswith("/group-create"))
        self.assertEqual(post.call_args.kwargs["json"], {"name": "Family", "participants": ["+94771234567"]})
        self.assertEqual(result["jid"], CHAT)

    def test_update_group_sends_only_given_fields(self):
        post, _ = self.post(lambda: whatsapp.update_group(CHAT, description=""))
        self.assertEqual(post.call_args.kwargs["json"], {"group_jid": CHAT, "description": ""})
        post, _ = self.post(lambda: whatsapp.update_group(CHAT, name="Trip"))
        self.assertEqual(post.call_args.kwargs["json"], {"group_jid": CHAT, "name": "Trip"})

    def test_participants_and_links(self):
        post, _ = self.post(lambda: whatsapp.update_group_participants(CHAT, ["123"], "promote"))
        self.assertEqual(post.call_args.kwargs["json"], {"group_jid": CHAT, "participants": ["123"], "action": "promote"})
        post, _ = self.post(lambda: whatsapp.get_group_invite_link(CHAT, reset=True))
        self.assertEqual(post.call_args.kwargs["json"], {"group_jid": CHAT, "reset": True})
        post, _ = self.post(lambda: whatsapp.join_group("https://chat.whatsapp.com/AbC"))
        self.assertTrue(post.call_args.args[0].endswith("/group-join"))
        post, _ = self.post(lambda: whatsapp.leave_group(CHAT))
        self.assertTrue(post.call_args.args[0].endswith("/group-leave"))

    def test_bridge_errors_are_readable(self):
        _, result = self.post(lambda: whatsapp.update_group_participants(CHAT, ["123"], "kick"),
                              {"success": False, "message": 'action must be add, remove, promote or demote, got "kick"'}, 400)
        self.assertFalse(result["success"])
        self.assertIn("action must be", result["message"])

        response = bridge_response(400, text="Invalid request format\n")
        with patch.object(whatsapp.requests, "post", return_value=response):
            result = whatsapp.leave_group(CHAT)
        self.assertEqual(result, {"success": False, "message": "Error: HTTP 400 - Invalid request format"})

        with patch.object(whatsapp.requests, "post", side_effect=whatsapp.requests.ConnectionError("refused")):
            result = whatsapp.leave_group(CHAT)
        self.assertIn("Request error", result["message"])

class PollTests(unittest.TestCase):
    def setUp(self):
        fd, self.db_path = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        conn = sqlite3.connect(self.db_path)
        conn.executescript("""
            CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
            CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP,
                is_from_me BOOLEAN, media_type TEXT, filename TEXT, url TEXT, media_key BLOB, file_sha256 BLOB,
                file_enc_sha256 BLOB, file_length INTEGER, quoted_message_id TEXT, quoted_sender TEXT,
                quoted_content TEXT, edited_at TIMESTAMP, is_deleted BOOLEAN NOT NULL DEFAULT 0,
                PRIMARY KEY (id, chat_jid));
            CREATE TABLE reactions (message_id TEXT, chat_jid TEXT, sender TEXT, emoji TEXT, timestamp TIMESTAMP,
                PRIMARY KEY (message_id, chat_jid, sender));
            CREATE TABLE polls (message_id TEXT, chat_jid TEXT, question TEXT, options TEXT, selectable_count INTEGER,
                PRIMARY KEY (message_id, chat_jid));
            CREATE TABLE poll_votes (poll_id TEXT, chat_jid TEXT, voter TEXT, options TEXT, timestamp TIMESTAMP,
                PRIMARY KEY (poll_id, chat_jid, voter));
        """)
        conn.execute("INSERT INTO chats VALUES (?, 'Lunch club', '2026-10-30 10:05:00+00:00')", (CHAT,))
        conn.execute("INSERT INTO chats VALUES ('456@s.whatsapp.net', 'Sam', '2026-10-30 09:00:00+00:00')")
        conn.execute("""INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me)
                        VALUES ('p1', ?, '123', 'Lunch where?', '2026-10-30 10:00:00+00:00', 0)""", (CHAT,))
        conn.execute("""INSERT INTO polls VALUES ('p1', ?, 'Lunch where?', '["Pizza", "Sushi", "Tacos"]', 1)""", (CHAT,))
        votes = [("456", '["Pizza"]'), ("789", '["Pizza"]'), ("999", '["Sushi"]'), ("555", "[]")]
        for voter, options in votes:
            conn.execute("INSERT INTO poll_votes VALUES ('p1', ?, ?, ?, '2026-10-30 10:01:00+00:00')", (CHAT, voter, options))
        conn.commit()
        conn.close()
        patcher = patch.object(whatsapp, "MESSAGES_DB_PATH", self.db_path)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(os.remove, self.db_path)

    def test_poll_results(self):
        result = whatsapp.get_poll_results(CHAT, "p1")
        self.assertTrue(result["success"])
        self.assertEqual(result["question"], "Lunch where?")
        self.assertFalse(result["multiple_answers"])
        self.assertEqual(result["voters"], 3)
        counts = {o["option"]: o["votes"] for o in result["options"]}
        self.assertEqual(counts, {"Pizza": 2, "Sushi": 1, "Tacos": 0})
        pizza = next(o for o in result["options"] if o["option"] == "Pizza")
        self.assertIn("Sam", pizza["voted_by"])

    def test_unknown_poll(self):
        result = whatsapp.get_poll_results(CHAT, "nope")
        self.assertFalse(result["success"])

    def test_poll_shows_in_messages(self):
        output = whatsapp.list_messages(chat_jid=CHAT, include_context=False)
        self.assertIn("[poll - Message ID: p1 - Chat JID: 120363@g.us] Lunch where? (options: Pizza / Sushi / Tacos)", output)

    def test_create_and_vote_payloads(self):
        ok = bridge_response(200, {"success": True, "message": "Poll sent"})
        with patch.object(whatsapp.requests, "post", return_value=ok) as post:
            whatsapp.create_poll(CHAT, "Lunch where?", ["Pizza", "Sushi"], multiple_answers=True)
            self.assertTrue(post.call_args.args[0].endswith("/poll"))
            self.assertEqual(post.call_args.kwargs["json"],
                             {"recipient": CHAT, "question": "Lunch where?", "options": ["Pizza", "Sushi"], "multiple_answers": True})
            whatsapp.vote_poll(CHAT, "p1", [])
            self.assertTrue(post.call_args.args[0].endswith("/poll-vote"))
            self.assertEqual(post.call_args.kwargs["json"], {"chat_jid": CHAT, "message_id": "p1", "options": []})

if __name__ == '__main__':
    unittest.main()
