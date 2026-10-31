import unittest
from unittest.mock import Mock, patch
import whatsapp

class ContactLookupTests(unittest.TestCase):
    def test_check_numbers_summarises_results(self):
        response = Mock(status_code=200)
        response.json.return_value = [
            {"phone": "94771234567", "on_whatsapp": True, "jid": "94771234567@s.whatsapp.net"},
            {"phone": "94770000000", "on_whatsapp": False},
        ]
        with patch.object(whatsapp.requests, "post", return_value=response) as post:
            result = whatsapp.check_whatsapp_numbers(["+94771234567", "+94770000000"])
        self.assertEqual(post.call_args.kwargs["json"], {"phones": ["+94771234567", "+94770000000"]})
        self.assertTrue(result["success"])
        self.assertEqual(result["message"], "1 of 2 numbers are on WhatsApp")
        self.assertEqual(len(result["results"]), 2)

    def test_check_numbers_reports_bridge_rejection(self):
        response = Mock(status_code=400, text="invalid phone number \"077\"\n")
        with patch.object(whatsapp.requests, "post", return_value=response):
            result = whatsapp.check_whatsapp_numbers(["077"])
        self.assertFalse(result["success"])
        self.assertIn("invalid phone number", result["message"])

    def test_check_numbers_requires_input(self):
        self.assertFalse(whatsapp.check_whatsapp_numbers([])["success"])

    def test_contact_info_escapes_path(self):
        response = Mock(status_code=200)
        response.json.return_value = {"jid": "94771234567@s.whatsapp.net", "about": "Hey there"}
        with patch.object(whatsapp.requests, "get", return_value=response) as get:
            result = whatsapp.get_contact_info(" +94 77 123 4567 ")
        self.assertTrue(get.call_args.args[0].endswith("/contact/+94%2077%20123%204567"))
        self.assertTrue(result["success"])
        self.assertEqual(result["about"], "Hey there")

    def test_contact_info_not_found(self):
        response = Mock(status_code=404, text="Not on WhatsApp: 94770000000@s.whatsapp.net")
        with patch.object(whatsapp.requests, "get", return_value=response):
            result = whatsapp.get_contact_info("+94770000000")
        self.assertFalse(result["success"])
        self.assertIn("Not on WhatsApp", result["message"])

if __name__ == '__main__':
    unittest.main()
