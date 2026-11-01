import unittest
from unittest.mock import Mock, patch
import whatsapp

def bridge_response(status_code, body=None, text=""):
    response = Mock(status_code=status_code, text=text)
    if body is None:
        response.json.side_effect = ValueError("not JSON")
    else:
        response.json.return_value = body
    return response

class ReplyTests(unittest.TestCase):
    def test_reply_to_is_sent_only_when_given(self):
        ok = bridge_response(200, {"success": True, "message": "Message sent to 123"})
        with patch.object(whatsapp.requests, "post", return_value=ok) as post:
            whatsapp.send_message("123", "hi")
            self.assertNotIn("reply_to", post.call_args.kwargs["json"])
            success, _ = whatsapp.send_message("123@s.whatsapp.net", "yes", reply_to="m1")
        self.assertTrue(success)
        self.assertEqual(post.call_args.kwargs["json"]["reply_to"], "m1")

    def test_file_reply(self):
        ok = bridge_response(200, {"success": True, "message": "sent"})
        with patch.object(whatsapp.os.path, "isfile", return_value=True), \
                patch.object(whatsapp.requests, "post", return_value=ok) as post:
            whatsapp.send_file("123@s.whatsapp.net", "/tmp/a.jpg", reply_to="m1")
        self.assertEqual(post.call_args.kwargs["json"]["reply_to"], "m1")

    def test_bridge_failure_message_is_readable(self):
        failed = bridge_response(500, {"success": False, "message": "message m1 not found in chat 123@s.whatsapp.net"},
                                 text='{"success":false,"message":"..."}')
        with patch.object(whatsapp.requests, "post", return_value=failed):
            success, message = whatsapp.send_message("123", "yes", reply_to="m1")
        self.assertFalse(success)
        self.assertEqual(message, "message m1 not found in chat 123@s.whatsapp.net")

class ReactionTests(unittest.TestCase):
    def test_reaction_payload(self):
        ok = bridge_response(200, {"success": True, "message": "Reacted 👍 to message m1"})
        with patch.object(whatsapp.requests, "post", return_value=ok) as post:
            success, message = whatsapp.send_reaction("123@s.whatsapp.net", "m1", " 👍 ")
        self.assertTrue(post.call_args.args[0].endswith("/react"))
        self.assertEqual(post.call_args.kwargs["json"], {"chat_jid": "123@s.whatsapp.net", "message_id": "m1", "emoji": "👍"})
        self.assertTrue(success)
        self.assertIn("Reacted", message)

    def test_empty_emoji_removes(self):
        ok = bridge_response(200, {"success": True, "message": "Removed reaction from message m1"})
        with patch.object(whatsapp.requests, "post", return_value=ok) as post:
            success, _ = whatsapp.send_reaction("123@s.whatsapp.net", "m1", "")
        self.assertTrue(success)
        self.assertEqual(post.call_args.kwargs["json"]["emoji"], "")

    def test_plain_text_rejection(self):
        rejected = bridge_response(400, text="Chat JID and message ID are required\n")
        with patch.object(whatsapp.requests, "post", return_value=rejected):
            success, message = whatsapp.send_reaction("123@s.whatsapp.net", "m1", "👍")
        self.assertFalse(success)
        self.assertEqual(message, "Error: HTTP 400 - Chat JID and message ID are required")

    def test_requires_ids(self):
        self.assertFalse(whatsapp.send_reaction("", "m1", "👍")[0])
        self.assertFalse(whatsapp.send_reaction("123@s.whatsapp.net", "", "👍")[0])

class EditDeleteTests(unittest.TestCase):
    def test_edit_payload(self):
        ok = bridge_response(200, {"success": True, "message": "Edited message s1"})
        with patch.object(whatsapp.requests, "post", return_value=ok) as post:
            success, _ = whatsapp.edit_message("123@s.whatsapp.net", "s1", "see you at 7")
        self.assertTrue(success)
        self.assertTrue(post.call_args.args[0].endswith("/edit"))
        self.assertEqual(post.call_args.kwargs["json"],
                         {"chat_jid": "123@s.whatsapp.net", "message_id": "s1", "text": "see you at 7"})

    def test_edit_requires_text(self):
        with patch.object(whatsapp.requests, "post") as post:
            success, _ = whatsapp.edit_message("123@s.whatsapp.net", "s1", "  ")
        self.assertFalse(success)
        post.assert_not_called()

    def test_edit_too_old_is_explained(self):
        failed = bridge_response(500, {"success": False, "message": "message s1 is too old to edit"})
        with patch.object(whatsapp.requests, "post", return_value=failed):
            success, message = whatsapp.edit_message("123@s.whatsapp.net", "s1", "fixed")
        self.assertFalse(success)
        self.assertEqual(message, "message s1 is too old to edit")

    def test_delete_payload(self):
        ok = bridge_response(200, {"success": True, "message": "Deleted message s1 for everyone"})
        with patch.object(whatsapp.requests, "post", return_value=ok) as post:
            success, _ = whatsapp.delete_message("123@s.whatsapp.net", "s1")
        self.assertTrue(success)
        self.assertTrue(post.call_args.args[0].endswith("/delete"))
        self.assertEqual(post.call_args.kwargs["json"], {"chat_jid": "123@s.whatsapp.net", "message_id": "s1"})

    def test_delete_requires_ids(self):
        self.assertFalse(whatsapp.delete_message("", "s1")[0])
        self.assertFalse(whatsapp.delete_message("123@s.whatsapp.net", "")[0])

if __name__ == '__main__':
    unittest.main()
