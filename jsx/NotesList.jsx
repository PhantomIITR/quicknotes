// http://blog.vjeux.com/2013/javascript/scroll-position-with-react.html
import React from 'react';
import ReactDOM from 'react-dom';
import Note from'./Note.jsx';
import * as ni from './noteinfo.js';

const maxInitialNotes = 50;

function truncateNotes(notes) {
  if (maxInitialNotes != -1 && notes.length >= maxInitialNotes) {
    return notes.slice(0, maxInitialNotes);
  }
  return notes;
}

export default class NotesList extends React.Component {
  constructor(props, context) {
    super(props, context);
    this.handleScroll = this.handleScroll.bind(this);

    this.state = {
      notes: truncateNotes(props.notes)
    };
  }

  componentWillReceiveProps(nextProps) {
    var node = ReactDOM.findDOMNode(this);
    node.scrollTop = 0;
    this.setState({
      notes: truncateNotes(nextProps.notes)
    });
  }

  handleScroll(e) {
    e.preventDefault();
    var nShowing = this.state.notes.length;
    var total = this.props.notes.length;
    if (nShowing >= total) {
      return;
    }
    var node = e.target;
    var top = node.scrollTop;
    var dy = node.scrollHeight;
    // a heuristic, maybe push it down
    var addMore = top > dy/2;
    if (!addMore) {
      return;
    }
    //console.log("top: " + top + " height: " + dy);
    var last = nShowing + 10;
    if (last > total) {
      last = total;
    }
    var notes = this.state.notes;
    for (var i = nShowing; i < last; i++) {
      notes.push(this.props.notes[i]);
    }
    //console.log("new number of notes: " + notes.length);
    this.setState({
      notes: notes,
    });
  }

  render() {
    var self = this;
    return (
      <div id="notes-list" onScroll={this.handleScroll}>
        <div className="wrapper">
          {this.state.notes.map(function(note) {
            return <Note
              compact={self.props.compact}
              note={note}
              key={ni.IDStr(note)}
              myNotes={self.props.myNotes}
              permanentDeleteNoteCb={self.props.permanentDeleteNoteCb}
              delUndelNoteCb={self.props.delUndelNoteCb}
              makeNotePublicPrivateCb={self.props.makeNotePublicPrivateCb}
              startUnstarNoteCb={self.props.startUnstarNoteCb}
              editCb={self.props.editCb}
            />;
          })}
        </div>
      </div>
    );
  }
}
