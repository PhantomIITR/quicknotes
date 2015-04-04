/* jshint -W097 */
'use strict';

function urlifyTitle(s) {
  s = s.slice(0,32);
  return s.toLowerCase().replace(/[^\w ]+/g,'').replace(/ +/g,'-');
}

var Note = React.createClass({

  getInitialState: function() {
    return {
      showActions: false
    };
  },

  createTitle: function(note) {
    if (note.Title !== "") {
      return (
        <span className="note-title">{note.Title}</span>
        );
    }
  },

  createTags: function(tags) {
    if (!tags) {
      return (
        <span></span>
      );
    }
    var tagEls = tags.map(function (tag) {
        tag = "#" + tag;
        return (
          <span key={tag} className="note-tag">{tag}</span>
        );
    });

    return (
      <div className="note-tags">{tagEls}</div>
    );
  },

  mouseEnter: function(e) {
    e.preventDefault();
    this.setState({
      showActions: true
    });
  },

  mouseLeave: function(e) {
    e.preventDefault();
    this.setState({
      showActions: false
    });
  },

  createNoteSnippet: function(note) {
    if (!this.props.compact) {
      return (
        <div className="content">
          <pre className="snippet">{note.Snippet}</pre>
        </div>
      );
    }
  },

  handleDelUndel: function(e) {
    e.preventDefault();
    this.props.delUndelNoteCb(this.props.note);
  },

  handleMakePublicPrivate: function(e) {
    e.preventDefault();
    var note = this.props.note;
    console.log("handleMakePublicPrivate, note.IsPublic: ", note.IsPublic);
    this.props.makeNotePublicPrivateCb(note);
  },

  createDelUndel: function(note) {
    if (note.IsDeleted) {
      return (
        <a href="#" className="noteLink" onClick={this.handleDelUndel}>undelete</a>
      );
    }
    return (
      <a href="#" className="noteLink" onClick={this.handleDelUndel}>delete</a>
    );
  },

  handleEdit: function(e) {
    e.preventDefault();
    //this.props.editCb(this.props.note);
  },

  createEdit: function(note) {
    if (!note.IsDeleted) {
      return (
        <a href="#" className="noteLink" onClick={this.handleEdit}>edit</a>
      );
    } else {
      return (
        <span></span>
      );
    }
  },

  createViewLink: function(note) {
    var txt = "view";
    var title = "";
    if (note.Title.length > 0) {
      title = "-" + urlifyTitle(note.Title);
    }
    var url = "/n/" + note.IDStr + title;
    return (
      <a href={url} className="noteLink" target="_blank">{txt}</a>
    );
  },

  createSize: function(note) {
    return (
      <span>{note.HumanSize}</span>
    );
  },

  createMakePublicPrivate: function(note) {
    if (note.IsPublic) {
      return (
        <a href="#" className="noteLink" onClick={this.handleMakePublicPrivate}>make private</a>
      );
    } else {
      return (
        <a href="#" className="noteLink" onClick={this.handleMakePublicPrivate}>make public</a>
      );
    }
  },

  handleStarUnstarNote: function(e) {
    e.preventDefault();
    var note = this.props.note;
    console.log("handleStarUnstarNote, note.IsStarred: ", note.IsStarred);
    this.props.startUnstarNoteCb(note);
  },

  createStarUnstar: function(note) {
    if (note.IsStarred) {
      return (
        <a href="#" className="noteLink" onClick={this.handleStarUnstarNote}>unstar</a>
      );
    } else {
      return (
        <a href="#" className="noteLink" onClick={this.handleStarUnstarNote}>star</a>
      );
    }
  },

  createActionsIfMyNotes: function(note) {
    if (this.state.showActions) {
      return (
       <span>
        {this.createDelUndel(note)}
        {this.createMakePublicPrivate(note)}
        {this.createStarUnstar(note)}
        {this.createEdit(note)}
        {this.createViewLink(note)}
        {this.createSize(note)}
      </span>
      );
    }
    return (
      <span></span>
    );
  },

  createActionsIfNotMyNotes: function(note) {
    if (this.state.showActions) {
      return (
       <span>
        {this.createViewLink(note)}
        {this.createSize(note)}
      </span>
      );
    }
    return (
      <span></span>
    );
  },

  createActions: function(note) {
    if (this.props.myNotes) {
        return this.createActionsIfMyNotes(note);
    } else {
      return this.createActionsIfNotMyNotes(note);
    }
  },

  render: function() {
    var note = this.props.note;
    return (
      <div className="note"
        onMouseEnter={this.mouseEnter}
        onMouseLeave={this.mouseLeave}
        >
        <div className="note-header">
          {this.createTitle(note)}
          {this.createActions(note)}
        </div>
        {this.createNoteSnippet(note)}
        {this.createTags(note.Tags)}
      </div>
    );
  }
});

module.exports = Note;
