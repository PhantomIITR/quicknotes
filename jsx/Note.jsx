/* jshint -W097,-W117 */
'use strict';

var React = require('react');
var ReactDOM = require('react-dom');

var ni = require('./noteinfo.js');
var action = require('./action.js');

function urlifyTitle(s) {
  s = s.slice(0, 32);
  return s.toLowerCase().replace(/[^\w ]+/g, '').replace(/ +/g, '-');
}

var NoteBody = React.createClass({

  getInitialState: function() {
    return {
      note: this.props.note
    };
  },

  expand: function() {
    var note = this.state.note;
    console.log("expand note", ni.IDStr(note));
    ni.Expand(note);
    var content = ni.Content(note, this.onContent);
    // if has content, change the state immediately.
    // if doesn't have content, it'll be changed in onContent.
    // if we always do it and there is no content, we'll get an ugly flash
    // due to 2 updates in quick succession.
    if (content) {
      this.setState({
        note: note
      });
    }
  },

  collapse: function() {
    var note = this.state.note;
    console.log("collapse note", ni.IDStr(note));
    ni.Collapse(note);
    this.setState({
      note: note
    });
  },

  renderCollapseOrExpand: function(note) {
    // if a note is not partial, there's neither collapse nor exapnd
    if (!ni.IsPartial(note)) {
      return;
    }

    if (ni.IsCollapsed(note)) {
      return (
        <a href="#" className="expand" onClick={this.expand}>Expand</a>
      );
    }

    return (
      <a href="#" className="collapse" onClick={this.collapse}>Collapse</a>
    );
  },

  onContent: function(note) {
    console.log("NoteBody.onContent");
    this.setState({
      note: note
    });
  },

  renderContent: function(note) {
    if (ni.IsCollapsed(note)) {
      return <pre className="note-body">{ni.Snippet(note)}</pre>;
    }
    return <pre className="note-body">{ni.Content(note, this.onContent)}</pre>;
  },

  render: function() {
    if (this.props.compact) {
      return;
    }
    var note = this.state.note;
    //console.log("NoteBody.render() note: ", ni.IDStr(note), "collapsed:", ni.IsCollapsed(note));
    return (
        <div className="note-content">
          {this.renderContent(note)}
          {this.renderCollapseOrExpand(note)}
        </div>
    );
  }

});

var Note = React.createClass({

  getInitialState: function() {
    return {
      showActions: false
    };
  },

  renderTitle: function(note) {
    var title = ni.Title(note);
    if (title !== "") {
      return (
        <span className="note-title">{title}</span>
      );
    }
  },

  handleTagClicked: function(e) {
    var tag = e.target.textContent.substr(1);
    action.tagSelected(tag);
  },

  renderTags: function(tags) {
    if (!tags) {
      return;
    }
    var self = this;
    var tagEls = tags.map(function(tag) {
      tag = "#" + tag;
      return (
        <span className="note-tag" key={tag} onClick={self.handleTagClicked}>{tag}</span>
      );
    });

    return (
      <span className="note-tags">{tagEls}</span>
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

  handleDelUndel: function(e) {
    this.props.delUndelNoteCb(this.props.note);
  },

  handlePermanentDelete: function() {
    this.props.permanentDeleteNoteCb(this.props.note);
  },

  handleMakePublicPrivate: function(e) {
    var note = this.props.note;
    console.log("handleMakePublicPrivate, note.IsPublic: ", ni.IsPublic(note));
    this.props.makeNotePublicPrivateCb(note);
  },

  renderTrashUntrash: function(note) {
    if (ni.IsDeleted(note)) {
      return (
        <a className="note-action" href="#" onClick={this.handleDelUndel} title="Undelete">
          <i className="fa fa-undo"></i>
        </a>
      );
    }
    return (
      <a className="note-action" href="#" onClick={this.handleDelUndel} title="Move to Trash">
        <i className="fa fa-trash-o"></i>
      </a>
    );
  },

  renderPermanentDelete: function(note) {
    if (ni.IsDeleted(note)) {
      return (
        <a className="note-action" href="#" onClick={this.handlePermanentDelete} title="Delete permanently">
          <i className="fa fa-trash-o"></i>
        </a>
      );
    }
  },

  handleEdit: function(e) {
    console.log("Note.handleEdit");
    this.props.editCb(this.props.note);
  },

  renderEdit: function(note) {
    if (!ni.IsDeleted(note)) {
      return (
        <a className="note-action" href="#" onClick={this.handleEdit} title="Edit note">
          <i className="fa fa-pencil"></i>
        </a>
      );
    }
  },

  renderViewLink: function(note) {
    var title = ni.Title(note);
    if (title.length > 0) {
      title = "-" + urlifyTitle(title);
    }
    var url = "/n/" + ni.IDStr(note) + title;
    return (
      <a className="note-action" href={url} target="_blank" title="View note">
        <i className="fa fa-external-link"></i>
      </a>
    );
  },

  renderSize: function(note) {
    return (
      <span className="note-size">{ni.HumanSize(note)}</span>
    );
  },

  renderMakePublicPrivate: function(note) {
    if (ni.IsDeleted) {
      return;
    }
    if (ni.IsPublic(note)) {
      return (
        <a className="note-action" href="#" onClick={this.handleMakePublicPrivate} title="Make private">
          <i className="fa fa-unlock"></i>
        </a>
      );
    } else {
      return (
        <a className="note-action" href="#" onClick={this.handleMakePublicPrivate} title="Make public">
          <i className="fa fa-lock"></i>
        </a>
      );
    }
  },

  handleStarUnstarNote: function(e) {
    var note = this.props.note;
    console.log("handleStarUnstarNote, note.IsStarred: ", ni.IsStarred(note));
    this.props.startUnstarNoteCb(note);
  },

  renderStarUnstar: function(note) {
    if (!this.props.myNotes || ni.IsDeleted((note))) {
      return;
    }

    var isStarred = ni.IsStarred(note);
    if (isStarred) {
      return (
        <a className="note-action note-star note-starred" href="#" onClick={this.handleStarUnstarNote} title="Unstar">
          <i className="fa fa-star"></i>
        </a>
      );
    } else {
      return (
        <a className="note-action note-star" href="#" onClick={this.handleStarUnstarNote} title="Star">
          <i className="fa fa-star-o"></i>
        </a>
      );
    }
  },

  renderActionsIfMyNotes: function(note) {
    if (this.state.showActions) {
      return (
        <div className="note-actions">
          {this.renderTrashUntrash(note)}
          {this.renderPermanentDelete(note)}
          {this.renderMakePublicPrivate(note)}
          {this.renderEdit(note)}
          {this.renderViewLink(note)}
        </div>
      );
    }
  },

  renderActionsIfNotMyNotes: function(note) {
    if (this.state.showActions) {
      return (
        <div className="note-actions">
          {this.renderViewLink(note)}
        </div>
      );
    }
    return (
      <div className="note-actions"></div>
    );
  },

  renderActions: function(note) {
    if (this.props.myNotes) {
      return this.renderActionsIfMyNotes(note);
    } else {
      return this.renderActionsIfNotMyNotes(note);
    }
  },

  render: function() {
    var note = this.props.note;
    return (
      <div className="note" onMouseEnter={this.mouseEnter} onMouseLeave={this.mouseLeave}>
        <div className="note-header">
          {this.renderStarUnstar(note)}
          {this.renderTitle(note)}
          {this.renderTags(ni.Tags(note))}
          {this.renderActions(note)}
        </div>
        <NoteBody compact={this.props.compact} note={note} />
      </div>
    );
  }
});

module.exports = Note;
