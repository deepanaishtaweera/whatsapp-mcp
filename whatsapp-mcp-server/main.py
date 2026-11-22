from typing import List, Dict, Any, Optional
from mcp.server.fastmcp import FastMCP
from whatsapp import (
    search_contacts as whatsapp_search_contacts,
    list_messages as whatsapp_list_messages,
    list_chats as whatsapp_list_chats,
    get_chat as whatsapp_get_chat,
    get_direct_chat_by_contact as whatsapp_get_direct_chat_by_contact,
    get_group_participants as whatsapp_get_group_participants,
    get_contact_chats as whatsapp_get_contact_chats,
    get_last_interaction as whatsapp_get_last_interaction,
    get_message_context as whatsapp_get_message_context,
    send_message as whatsapp_send_message,
    send_file as whatsapp_send_file,
    send_audio_message as whatsapp_audio_voice_message,
    send_status as whatsapp_send_status,
    send_reaction as whatsapp_send_reaction,
    create_group as whatsapp_create_group,
    update_group_participants as whatsapp_update_group_participants,
    update_group as whatsapp_update_group,
    get_group_invite_link as whatsapp_get_group_invite_link,
    leave_group as whatsapp_leave_group,
    join_group as whatsapp_join_group,
    create_poll as whatsapp_create_poll,
    vote_poll as whatsapp_vote_poll,
    get_poll_results as whatsapp_get_poll_results,
    edit_message as whatsapp_edit_message,
    delete_message as whatsapp_delete_message,
    check_whatsapp_numbers as whatsapp_check_whatsapp_numbers,
    get_contact_info as whatsapp_get_contact_info,
    download_media as whatsapp_download_media
)

# Initialize FastMCP server
mcp = FastMCP("whatsapp")

@mcp.tool()
def search_contacts(query: str) -> List[Dict[str, Any]]:
    """Search WhatsApp contacts by name or phone number.
    
    Args:
        query: Search term to match against contact names or phone numbers
    """
    contacts = whatsapp_search_contacts(query)
    return contacts

@mcp.tool()
def list_messages(
    after: Optional[str] = None,
    before: Optional[str] = None,
    sender_phone_number: Optional[str] = None,
    chat_jid: Optional[str] = None,
    query: Optional[str] = None,
    limit: int = 20,
    page: int = 0,
    include_context: bool = True,
    context_before: int = 1,
    context_after: int = 1
) -> List[Dict[str, Any]]:
    """Get WhatsApp messages matching specified criteria with optional context.
    
    Args:
        after: Optional ISO-8601 formatted string to only return messages after this date
        before: Optional ISO-8601 formatted string to only return messages before this date
        sender_phone_number: Optional phone number to filter messages by sender
        chat_jid: Optional chat JID to filter messages by chat
        query: Optional search term to filter messages by content
        limit: Maximum number of messages to return (default 20)
        page: Page number for pagination (default 0)
        include_context: Whether to include messages before and after matches (default True)
        context_before: Number of messages to include before each match (default 1)
        context_after: Number of messages to include after each match (default 1)
    """
    messages = whatsapp_list_messages(
        after=after,
        before=before,
        sender_phone_number=sender_phone_number,
        chat_jid=chat_jid,
        query=query,
        limit=limit,
        page=page,
        include_context=include_context,
        context_before=context_before,
        context_after=context_after
    )
    return messages

@mcp.tool()
def list_chats(
    query: Optional[str] = None,
    limit: int = 20,
    page: int = 0,
    include_last_message: bool = True,
    sort_by: str = "last_active"
) -> List[Dict[str, Any]]:
    """Get WhatsApp chats matching specified criteria.
    
    Args:
        query: Optional search term to filter chats by name or JID
        limit: Maximum number of chats to return (default 20)
        page: Page number for pagination (default 0)
        include_last_message: Whether to include the last message in each chat (default True)
        sort_by: Field to sort results by, either "last_active" or "name" (default "last_active")
    """
    chats = whatsapp_list_chats(
        query=query,
        limit=limit,
        page=page,
        include_last_message=include_last_message,
        sort_by=sort_by
    )
    return chats

@mcp.tool()
def get_chat(chat_jid: str, include_last_message: bool = True) -> Dict[str, Any]:
    """Get WhatsApp chat metadata by JID.
    
    Args:
        chat_jid: The JID of the chat to retrieve
        include_last_message: Whether to include the last message (default True)
    """
    chat = whatsapp_get_chat(chat_jid, include_last_message)
    return chat

@mcp.tool()
def get_direct_chat_by_contact(sender_phone_number: str) -> Dict[str, Any]:
    """Get WhatsApp chat metadata by sender phone number.
    
    Args:
        sender_phone_number: The phone number to search for
    """
    chat = whatsapp_get_direct_chat_by_contact(sender_phone_number)
    return chat

@mcp.tool()
def get_group_participants(chat_jid: str) -> Dict[str, Any]:
    """List the contacts that are members of a WhatsApp group.

    Requires the WhatsApp bridge to be running and connected.

    Args:
        chat_jid: The group's JID (e.g., "123456789@g.us"), as returned by list_chats

    Returns:
        A dictionary containing success status, a status message, the group metadata
        and the list of participants with their names, phone numbers and admin status
    """
    success, status_message, group = whatsapp_get_group_participants(chat_jid)

    if not success or group is None:
        return {
            "success": False,
            "message": status_message
        }

    return {
        "success": True,
        "message": status_message,
        "jid": group.jid,
        "name": group.name,
        "topic": group.topic,
        "owner_jid": group.owner_jid,
        "created": group.created.isoformat() if group.created else None,
        "participant_count": group.participant_count,
        "participants": [
            {
                "jid": participant.jid,
                "phone_number": participant.phone_number,
                "name": participant.name,
                "is_admin": participant.is_admin,
                "is_super_admin": participant.is_super_admin,
            }
            for participant in group.participants
        ]
    }

@mcp.tool()
def get_contact_chats(jid: str, limit: int = 20, page: int = 0) -> List[Dict[str, Any]]:
    """Get all WhatsApp chats involving the contact.
    
    Args:
        jid: The contact's JID to search for
        limit: Maximum number of chats to return (default 20)
        page: Page number for pagination (default 0)
    """
    chats = whatsapp_get_contact_chats(jid, limit, page)
    return chats

@mcp.tool()
def get_last_interaction(jid: str) -> str:
    """Get most recent WhatsApp message involving the contact.
    
    Args:
        jid: The JID of the contact to search for
    """
    message = whatsapp_get_last_interaction(jid)
    return message

@mcp.tool()
def get_message_context(
    message_id: str,
    before: int = 5,
    after: int = 5
) -> Dict[str, Any]:
    """Get context around a specific WhatsApp message.
    
    Args:
        message_id: The ID of the message to get context for
        before: Number of messages to include before the target message (default 5)
        after: Number of messages to include after the target message (default 5)
    """
    context = whatsapp_get_message_context(message_id, before, after)
    return context

@mcp.tool()
def check_whatsapp_numbers(phone_numbers: List[str]) -> Dict[str, Any]:
    """Check which phone numbers are registered on WhatsApp, e.g. before messaging a new number.

    Requires the WhatsApp bridge to be running and connected. Checks at most 50 numbers per
    call; WhatsApp limits these lookups, so don't use it to scan large lists.

    Args:
        phone_numbers: Phone numbers in international format with country code,
                       e.g. ["+94771234567", "+1 415 555 0100"]

    Returns:
        A dictionary containing success status, a summary message, and per number whether it is
        on WhatsApp, its JID (usable as a send_message recipient) and any verified business name
    """
    return whatsapp_check_whatsapp_numbers(phone_numbers)

@mcp.tool()
def get_contact_info(contact: str) -> Dict[str, Any]:
    """Get a contact's WhatsApp profile: name, about text, business name and profile picture.

    Requires the WhatsApp bridge to be running and connected. Fields the contact hides
    with their privacy settings are left out.

    Args:
        contact: A phone number with country code (e.g. "+94771234567") or a JID
                 (e.g. "94771234567@s.whatsapp.net")

    Returns:
        A dictionary containing success status, a status message, and the contact's jid,
        phone_number, name (from your address book), push_name (the name they set),
        business_name, about, and picture_url or picture_note
    """
    return whatsapp_get_contact_info(contact)

@mcp.tool()
def send_message(
    recipient: str,
    message: str,
    reply_to: Optional[str] = None
) -> Dict[str, Any]:
    """Send a WhatsApp message to a person or group. For group chats use the JID.

    Args:
        recipient: The recipient - either a phone number with country code but no + or other symbols,
                 or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        message: The message text to send
        reply_to: Optional ID of a message to reply to, as shown by list_messages. When replying,
                 use that message's chat JID as the recipient.

    Returns:
        A dictionary containing success status and a status message with the sent message's ID
    """
    # Validate input
    if not recipient:
        return {
            "success": False,
            "message": "Recipient must be provided"
        }
    
    # Call the whatsapp_send_message function with the unified recipient parameter
    success, status_message = whatsapp_send_message(recipient, message, reply_to)
    return {
        "success": success,
        "message": status_message
    }

@mcp.tool()
def send_file(recipient: str, media_path: str, reply_to: Optional[str] = None) -> Dict[str, Any]:
    """Send a file such as a picture, raw audio, video or document via WhatsApp to the specified recipient. For group messages use the JID.

    Args:
        recipient: The recipient - either a phone number with country code but no + or other symbols,
                 or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        media_path: The absolute path to the media file to send (image, video, document)
        reply_to: Optional ID of a message to reply to, as shown by list_messages. When replying,
                 use that message's chat JID as the recipient.

    Returns:
        A dictionary containing success status and a status message with the sent message's ID
    """

    # Call the whatsapp_send_file function
    success, status_message = whatsapp_send_file(recipient, media_path, reply_to)
    return {
        "success": success,
        "message": status_message
    }

@mcp.tool()
def send_reaction(chat_jid: str, message_id: str, emoji: str) -> Dict[str, Any]:
    """React to a WhatsApp message with an emoji, or remove your reaction.

    Args:
        chat_jid: The JID of the chat the message is in, as shown by list_messages
        message_id: The ID of the message to react to
        emoji: A single emoji such as "👍". Pass an empty string to remove your reaction.

    Returns:
        A dictionary containing success status and a status message
    """
    success, status_message = whatsapp_send_reaction(chat_jid, message_id, emoji)
    return {
        "success": success,
        "message": status_message
    }

@mcp.tool()
def edit_message(chat_jid: str, message_id: str, text: str) -> Dict[str, Any]:
    """Edit the text of a message you sent. WhatsApp only allows editing text messages
    within 15 minutes of sending them.

    Args:
        chat_jid: The JID of the chat the message is in, as shown by list_messages
        message_id: The ID of your message, as shown by list_messages or returned by send_message
        text: The new text of the message

    Returns:
        A dictionary containing success status and a status message
    """
    success, status_message = whatsapp_edit_message(chat_jid, message_id, text)
    return {
        "success": success,
        "message": status_message
    }

@mcp.tool()
def delete_message(chat_jid: str, message_id: str) -> Dict[str, Any]:
    """Delete a message you sent for everyone in the chat. WhatsApp only allows this for
    about 2.5 days after sending. This cannot be undone.

    Args:
        chat_jid: The JID of the chat the message is in, as shown by list_messages
        message_id: The ID of your message, as shown by list_messages or returned by send_message

    Returns:
        A dictionary containing success status and a status message
    """
    success, status_message = whatsapp_delete_message(chat_jid, message_id)
    return {
        "success": success,
        "message": status_message
    }

@mcp.tool()
def send_audio_message(recipient: str, media_path: str) -> Dict[str, Any]:
    """Send any audio file as a WhatsApp audio message to the specified recipient. For group messages use the JID. If it errors due to ffmpeg not being installed, use send_file instead.
    
    Args:
        recipient: The recipient - either a phone number with country code but no + or other symbols,
                 or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        media_path: The absolute path to the audio file to send (will be converted to Opus .ogg if it's not a .ogg file)
    
    Returns:
        A dictionary containing success status and a status message
    """
    success, status_message = whatsapp_audio_voice_message(recipient, media_path)
    return {
        "success": success,
        "message": status_message
    }

@mcp.tool()
def create_group(name: str, participants: List[str]) -> Dict[str, Any]:
    """Create a WhatsApp group with you as its admin.

    Args:
        name: The group name, up to 25 characters
        participants: Phone numbers with country code (e.g. "+94771234567") or JIDs of the people to add

    Returns:
        A dictionary with the new group's JID and, for each participant, whether they were added.
        People whose privacy settings block being added are offered an invite by WhatsApp instead.
    """
    return whatsapp_create_group(name, participants)

@mcp.tool()
def update_group_participants(group_jid: str, participants: List[str], action: str) -> Dict[str, Any]:
    """Add or remove group members, or make them admins or not. You must be a group admin.

    Args:
        group_jid: The group's JID (ends in @g.us), as returned by list_chats
        participants: Phone numbers with country code or JIDs of the members to change
        action: "add", "remove", "promote" (make admin) or "demote" (remove admin)

    Returns:
        A dictionary with, for each participant, whether the change worked and why not
    """
    return whatsapp_update_group_participants(group_jid, participants, action)

@mcp.tool()
def update_group(group_jid: str, name: Optional[str] = None, description: Optional[str] = None) -> Dict[str, Any]:
    """Rename a group or change its description. Leave out what shouldn't change;
    an empty description clears it.

    Args:
        group_jid: The group's JID (ends in @g.us), as returned by list_chats
        name: Optional new group name, up to 25 characters
        description: Optional new group description

    Returns:
        A dictionary containing success status and a status message
    """
    return whatsapp_update_group(group_jid, name, description)

@mcp.tool()
def get_group_invite_link(group_jid: str, reset: bool = False) -> Dict[str, Any]:
    """Get a group's invite link. Only group admins can do this.

    Args:
        group_jid: The group's JID (ends in @g.us), as returned by list_chats
        reset: Set to true to replace the link with a new one, so the old link stops working

    Returns:
        A dictionary with the invite link
    """
    return whatsapp_get_group_invite_link(group_jid, reset)

@mcp.tool()
def leave_group(group_jid: str) -> Dict[str, Any]:
    """Leave a WhatsApp group. Ask the user to confirm first: rejoining needs an invite.

    Args:
        group_jid: The group's JID (ends in @g.us), as returned by list_chats

    Returns:
        A dictionary containing success status and a status message
    """
    return whatsapp_leave_group(group_jid)

@mcp.tool()
def join_group(invite_link: str) -> Dict[str, Any]:
    """Join a WhatsApp group using an invite link.

    Args:
        invite_link: A link like "https://chat.whatsapp.com/AbCdEf123", or just the code at its end

    Returns:
        A dictionary with the group's JID and name
    """
    return whatsapp_join_group(invite_link)

@mcp.tool()
def create_poll(recipient: str, question: str, options: List[str], multiple_answers: bool = False) -> Dict[str, Any]:
    """Send a poll to a person or group.

    Args:
        recipient: A phone number with country code but no + or other symbols, or a JID
                 (e.g. "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
        question: The poll question
        options: 2 to 12 distinct answers
        multiple_answers: Set to true to let people pick more than one answer

    Returns:
        A dictionary containing success status and a status message with the poll's message ID
    """
    return whatsapp_create_poll(recipient, question, options, multiple_answers)

@mcp.tool()
def vote_poll(chat_jid: str, message_id: str, options: List[str]) -> Dict[str, Any]:
    """Vote on a poll, replacing any earlier vote. Pass an empty list to take your vote back.

    Args:
        chat_jid: The JID of the chat the poll is in, as shown by list_messages
        message_id: The poll's message ID, as shown by list_messages
        options: The answers to choose, spelled exactly as in the poll

    Returns:
        A dictionary containing success status and a status message
    """
    return whatsapp_vote_poll(chat_jid, message_id, options)

@mcp.tool()
def get_poll_results(chat_jid: str, message_id: str) -> Dict[str, Any]:
    """Get the current votes on a poll: the count for each answer and who chose it.
    Votes are counted from what the bridge has received since it was linked.

    Args:
        chat_jid: The JID of the chat the poll is in, as shown by list_messages
        message_id: The poll's message ID, as shown by list_messages

    Returns:
        A dictionary with the question, the number of voters, and each option's votes
    """
    return whatsapp_get_poll_results(chat_jid, message_id)

@mcp.tool()
def send_status(
    message: str = "",
    media_path: Optional[str] = None,
    background_color: Optional[str] = None
) -> Dict[str, Any]:
    """Post a WhatsApp status update (story). It is shown to the contacts allowed by the account's status privacy setting.

    Args:
        message: The status text, or the caption when media_path is given
        media_path: Optional absolute path to an image (jpg, png, webp) or video (mp4, avi, mov) to post; convert GIFs to mp4
        background_color: Optional background for a text status as "#RRGGBB" (defaults to dark teal); ignored for media

    Returns:
        A dictionary containing success status and a status message
    """
    success, status_message = whatsapp_send_status(message, media_path, background_color)
    return {
        "success": success,
        "message": status_message
    }

@mcp.tool()
def download_media(message_id: str, chat_jid: str) -> Dict[str, Any]:
    """Download media from a WhatsApp message and get the local file path.
    
    Args:
        message_id: The ID of the message containing the media
        chat_jid: The JID of the chat containing the message
    
    Returns:
        A dictionary containing success status, a status message, and the file path if successful
    """
    return whatsapp_download_media(message_id, chat_jid)

if __name__ == "__main__":
    mcp.run(transport="stdio")
